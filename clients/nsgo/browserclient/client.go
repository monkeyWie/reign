package browserclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Object = map[string]any
type Pairing struct {
	Relay, Host, Key, Token string
	RouteName               string
	ICE                     []string
	RelayOnly               bool
}
type frame struct {
	AllowRelay bool   `json:"allowRelay"`
	Peer       string `json:"peer"`
	Kind       string `json:"kind"`
	Body       []byte `json:"body"`
}
type chunk struct {
	ID    string `json:"id"`
	Index int    `json:"i"`
	Total int    `json:"n"`
	Data  []byte `json:"b"`
}
type assembly struct {
	parts       [][]byte
	count, size int
	created     time.Time
}
type peer interface {
	Offer() error
	Signal(Object) error
	Send([]byte) error
	Direct() bool
	Close()
}
type cached struct {
	revision string
	result   any
	bytes    int
}
type Client struct {
	mu, sending  sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	ws           *websocket.Conn
	dataWS       *websocket.Conn
	p            Pairing
	id, nonce    string
	base, crypto *cryptor
	ready        chan struct{}
	readyOnce    sync.Once
	pc           peer
	pending      map[string]chan Object
	chunks       map[string]*assembly
	cache        map[string]cached
	cacheBytes   int
}

func id() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func sum(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func Connect(ctx context.Context, p Pairing) (*Client, error) {
	u, e := url.Parse(p.Relay)
	rawKey, ke := hex.DecodeString(p.Key)
	if e != nil || u.User != nil || u.Host == "" || u.Scheme != "wss" && !(u.Scheme == "ws" && CleartextAllowed(u.Hostname())) || ke != nil || len(rawKey) != 32 || len(p.Host) < 16 || p.Token == "" {
		return nil, errors.New("无效或不安全的配对信息")
	}
	u.Path = "/p2p/signal"
	u.RawQuery = url.Values{"id": {p.Host}, "role": {"client"}}.Encode()
	ws, _, e := websocket.Dial(ctx, u.String(), &websocket.DialOptions{Subprotocols: []string{"reign.p2p", "auth." + base64.RawURLEncoding.EncodeToString([]byte(p.Token))}})
	if e != nil {
		return nil, e
	}
	ws.SetReadLimit(2 << 20)
	_, b, e := ws.Read(ctx)
	var f frame
	if e != nil || json.Unmarshal(b, &f) != nil || f.Kind != "welcome" || len(f.Peer) != 32 {
		ws.CloseNow()
		return nil, errors.New("无效中继握手")
	}
	// Connection lifetime must outlive the short pairing HTTP request.
	life, cancel := context.WithCancel(context.Background())
	c := &Client{ctx: life, cancel: cancel, ws: ws, p: p, id: f.Peer, nonce: id() + id(), ready: make(chan struct{}), pending: map[string]chan Object{}, chunks: map[string]*assembly{}, cache: map[string]cached{}}
	if f.AllowRelay {
		u.Path = "/p2p/relay"
		q := u.Query()
		q.Set("peer", f.Peer)
		u.RawQuery = q.Encode()
		c.dataWS, _, e = websocket.Dial(ctx, u.String(), &websocket.DialOptions{Subprotocols: []string{"reign.p2p", "auth." + base64.RawURLEncoding.EncodeToString([]byte(p.Token))}})
		if e != nil {
			c.Close()
			return nil, e
		}
		if _, _, e = c.dataWS.Read(ctx); e != nil {
			c.Close()
			return nil, e
		}
		go func() {
			for {
				if _, _, e := c.dataWS.Read(life); e != nil {
					return
				}
			}
		}()
	}
	c.base, e = newCryptor(sum("reign/p2p/peer/" + c.id + "/" + p.Key))
	if e != nil {
		c.Close()
		return nil, e
	}
	c.base.random = true
	go c.readLoop()
	hello, _ := json.Marshal(Object{"clientNonce": c.nonce})
	if e = c.relaySend("hello", hello); e != nil {
		c.Close()
		return nil, e
	}
	select {
	case <-c.ready:
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, errors.New("连接已断开")
	case <-time.After(20 * time.Second):
		c.Close()
		return nil, errors.New("连接握手超时")
	}
	if _, e = c.Call(ctx, Object{"method": "providers"}); e != nil {
		c.Close()
		return nil, e
	}
	if !p.RelayOnly {
		go func() {
			pc, e := newPeer(c, p.ICE)
			if e == nil {
				c.mu.Lock()
				if c.ctx.Err() != nil {
					c.mu.Unlock()
					pc.Close()
					return
				}
				c.pc = pc
				c.mu.Unlock()
				_ = pc.Offer()
			}
		}()
	}
	return c, nil
}
func (c *Client) aad(kind string, out bool) string {
	direction := "h2c"
	if out {
		direction = "c2h"
	}
	return "reign/p2p/" + c.id + "/" + direction + "/" + kind
}
func (c *Client) relaySend(kind string, b []byte) error {
	c.mu.Lock()
	crypto := c.crypto
	if kind == "hello" {
		crypto = c.base
	}
	c.mu.Unlock()
	if crypto == nil {
		return errors.New("握手未完成")
	}
	packet, e := crypto.seal(c.aad(kind, true), b)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(frame{Peer: c.id, Kind: kind, Body: packet})
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	socket := c.ws
	if kind == "chunk" || kind == "data" {
		socket = c.dataWS
		if socket == nil {
			return errors.New("直连失败，服务未提供中继权限")
		}
	}
	return socket.Write(ctx, websocket.MessageText, raw)
}
func (c *Client) readLoop() {
	defer c.Close()
	for {
		_, raw, e := c.ws.Read(c.ctx)
		if e != nil {
			return
		}
		var f frame
		if json.Unmarshal(raw, &f) != nil || f.Peer != c.id {
			continue
		}
		c.mu.Lock()
		crypto := c.crypto
		c.mu.Unlock()
		if f.Kind == "hello" && crypto == nil {
			plain, e := c.base.open(c.aad("hello", false), f.Body)
			if e != nil {
				continue
			}
			var hello struct{ ClientNonce, HostNonce string }
			if json.Unmarshal(plain, &hello) != nil || hello.ClientNonce != c.nonce || len(hello.HostNonce) != 64 {
				continue
			}
			if _, e = hex.DecodeString(hello.HostNonce); e != nil {
				continue
			}
			next, e := newCryptor(sum("reign/p2p/session/" + c.p.Key + "/" + c.id + "/" + c.nonce + "/" + hello.HostNonce))
			if e != nil {
				continue
			}
			next.prefix = [4]byte{1}
			c.mu.Lock()
			c.crypto = next
			c.mu.Unlock()
			c.readyOnce.Do(func() { close(c.ready) })
			continue
		}
		if crypto == nil {
			continue
		}
		switch f.Kind {
		case "chunk":
			_ = c.receiveChunk(f.Body)
		case "data":
			b, e := crypto.open(c.aad("data", false), f.Body)
			if e == nil {
				c.response(b)
			}
		case "signal":
			b, e := crypto.open(c.aad("signal", false), f.Body)
			if e != nil {
				continue
			}
			var v Object
			if json.Unmarshal(b, &v) != nil {
				continue
			}
			c.mu.Lock()
			pc := c.pc
			c.mu.Unlock()
			if pc != nil {
				_ = pc.Signal(v)
			}
		}
	}
}
func (c *Client) receiveChunk(packet []byte) error {
	c.mu.Lock()
	crypto := c.crypto
	c.mu.Unlock()
	if crypto == nil {
		return errors.New("握手未完成")
	}
	plain, e := crypto.open(c.aad("chunk", false), packet)
	if e != nil {
		return e
	}
	var part chunk
	if json.Unmarshal(plain, &part) != nil || part.Total < 1 || part.Total > 1400 || part.Index < 0 || part.Index >= part.Total || len(part.Data) > 12000 || len(part.ID) != 32 {
		return errors.New("无效分片")
	}
	c.mu.Lock()
	for k, a := range c.chunks {
		if time.Since(a.created) > 30*time.Second {
			delete(c.chunks, k)
		}
	}
	a := c.chunks[part.ID]
	if a == nil {
		if len(c.chunks) >= 8 {
			c.mu.Unlock()
			return errors.New("过多分片")
		}
		a = &assembly{parts: make([][]byte, part.Total), created: time.Now()}
		c.chunks[part.ID] = a
	}
	if len(a.parts) != part.Total || a.parts[part.Index] != nil {
		c.mu.Unlock()
		return nil
	}
	a.parts[part.Index] = part.Data
	a.count++
	a.size += len(part.Data)
	total := 0
	for _, x := range c.chunks {
		total += x.size
	}
	if a.size > 16<<20 || total > 20<<20 {
		delete(c.chunks, part.ID)
		c.mu.Unlock()
		return errors.New("分片过大")
	}
	if a.count != part.Total {
		c.mu.Unlock()
		return nil
	}
	delete(c.chunks, part.ID)
	out := make([]byte, 0, a.size)
	for _, p := range a.parts {
		out = append(out, p...)
	}
	c.mu.Unlock()
	c.response(out)
	return nil
}
func (c *Client) response(b []byte) {
	var v Object
	if json.Unmarshal(b, &v) != nil {
		return
	}
	id, _ := v["id"].(string)
	c.mu.Lock()
	pending := c.pending[id]
	c.mu.Unlock()
	if pending != nil {
		select {
		case pending <- v:
		default:
		}
	}
}
func (c *Client) Call(ctx context.Context, request Object) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	r := Object{}
	for k, v := range request {
		r[k] = v
	}
	raw, _ := json.Marshal(r)
	cacheKey := string(raw)
	method, _ := r["method"].(string)
	read := method == "providers" || method == "transcript" || method == "sessions" || method == "models" || method == "approvals" || method == "network"
	c.mu.Lock()
	previous := c.cache[cacheKey]
	c.mu.Unlock()
	if read && previous.revision != "" {
		r["revision"] = previous.revision
	}
	requestID, _ := r["id"].(string)
	if requestID == "" {
		requestID = id()
		r["id"] = requestID
	}
	pending := make(chan Object, 1)
	c.mu.Lock()
	if c.pending[requestID] != nil {
		c.mu.Unlock()
		return nil, errors.New("重复的指令 ID")
	}
	c.pending[requestID] = pending
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, requestID); c.mu.Unlock() }()
	raw, _ = json.Marshal(r)
	if len(raw) > 1<<20 {
		return nil, errors.New("指令过长")
	}
	if e := c.send(raw); e != nil {
		return nil, e
	}
	var response Object
	select {
	case response = <-pending:
	case <-ctx.Done():
		return nil, errors.New("结果尚未确认，请刷新后再决定是否重试")
	case <-c.ctx.Done():
		return nil, errors.New("连接已断开，指令结果尚未确认")
	}
	if e, _ := response["error"].(string); e != "" {
		return nil, errors.New(e)
	}
	if response["notModified"] == true {
		if response["revision"] != previous.revision || previous.revision == "" {
			return nil, errors.New("无效快照响应")
		}
		return previous.result, nil
	}
	result := response["result"]
	if read {
		revision, _ := response["revision"].(string)
		b, _ := json.Marshal(result)
		if revision != "" && len(b) <= 16<<20 {
			c.mu.Lock()
			if old, ok := c.cache[cacheKey]; ok {
				c.cacheBytes -= old.bytes
				delete(c.cache, cacheKey)
			}
			for k, v := range c.cache {
				if len(c.cache) < 32 && c.cacheBytes+len(b) <= 16<<20 {
					break
				}
				c.cacheBytes -= v.bytes
				delete(c.cache, k)
			}
			c.cache[cacheKey] = cached{revision, result, len(b)}
			c.cacheBytes += len(b)
			c.mu.Unlock()
		}
	}
	return result, nil
}
func (c *Client) send(raw []byte) error {
	c.sending.Lock()
	defer c.sending.Unlock()
	c.mu.Lock()
	pc, crypto := c.pc, c.crypto
	c.mu.Unlock()
	if crypto == nil || c.ctx.Err() != nil {
		return errors.New("连接已关闭")
	}
	direct := pc != nil && pc.Direct()
	count := (len(raw) + 11999) / 12000
	transfer := id()
	for i := 0; i < count; i++ {
		part, _ := json.Marshal(chunk{transfer, i, count, raw[i*12000 : min(len(raw), (i+1)*12000)]})
		if direct {
			packet, e := crypto.seal(c.aad("chunk", true), part)
			if e != nil {
				return e
			}
			if e = pc.Send(packet); e != nil {
				return fmt.Errorf("直连发送失败，结果未确认，请先刷新：%w", e)
			}
		} else if e := c.relaySend("chunk", part); e != nil {
			return e
		}
	}
	return nil
}
func (c *Client) Path() string {
	if c.ctx.Err() != nil {
		return "Disconnected"
	}
	c.mu.Lock()
	pc := c.pc
	c.mu.Unlock()
	if pc != nil && pc.Direct() {
		return "P2P · WebRTC"
	}
	if c.p.RouteName != "" {
		return "Relay · " + c.p.RouteName
	}
	return "Relay · encrypted"
}
func (c *Client) Connected() bool {
	return c.ctx.Err() == nil
}
func (c *Client) Close() {
	c.cancel()
	c.ws.CloseNow()
	c.mu.Lock()
	pc := c.pc
	c.pc = nil
	c.chunks = map[string]*assembly{}
	c.mu.Unlock()
	if pc != nil {
		pc.Close()
	}
}
func (c *Client) signal(v Object) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return c.relaySend("signal", b)
}
func STUN(policy Object) []string {
	values := []string{}
	seen := map[string]bool{}
	for _, key := range []string{"publicStun", "selfStun"} {
		switch v := policy[key].(type) {
		case []any:
			for _, raw := range v {
				if s, ok := raw.(string); ok && strings.HasPrefix(s, "stun:") && !seen[s] && len(values) < 16 {
					seen[s] = true
					values = append(values, s)
				}
			}
		case []string:
			for _, s := range v {
				if strings.HasPrefix(s, "stun:") && !seen[s] && len(values) < 16 {
					seen[s] = true
					values = append(values, s)
				}
			}
		}
	}
	if len(values) == 0 {
		return []string{"stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"}
	}
	return values
}
