// Package webapi adapts the unchanged Agent Hub protocol to browser storage.
package webapi

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/monkeyWie/reign/clients/nsgo/browserclient"
	"github.com/monkeyWie/reign/pkg/link"
	"strings"
	"sync"
	"time"
)

type Storage interface {
	Get(string) (any, error)
	Put(string, any) error
	Delete(string) error
	Keys() ([]string, error)
}
type API struct {
	store                      Storage
	mu, edit, identity, assets sync.Mutex
	clients                    map[string]*browserclient.Client
	busy                       map[string]bool
	generation                 map[string]int
	closed                     bool
}

func New(s Storage) *API {
	return &API{store: s, clients: map[string]*browserclient.Client{}, busy: map[string]bool{}, generation: map[string]int{}}
}
func (*API) CanHost() bool { return false }
func (a *API) Close() {
	a.mu.Lock()
	a.closed = true
	cs := a.clients
	a.clients = map[string]*browserclient.Client{}
	for id := range a.busy {
		a.generation[id]++
	}
	a.mu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}
func (a *API) saved() ([]Object, error) { v, e := a.store.Get("devices"); return arr(v), e }
func (a *API) editDevices(fn func([]Object) []Object) error {
	a.edit.Lock()
	defer a.edit.Unlock()
	v, e := a.saved()
	if e != nil {
		return e
	}
	return a.store.Put("devices", fn(v))
}
func (a *API) list() (any, error) {
	v, e := a.saved()
	if e != nil {
		return nil, e
	}
	out := []Object{}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range v {
		r := clone(d)
		delete(r, "key")
		c := a.clients[str(d, "id")]
		r["path"] = "离线"
		r["connected"] = false
		if c != nil {
			r["path"] = c.Path()
			r["connected"] = c.Connected()
		}
		r["autoReconnect"] = d["reconnectDisabled"] != true && str(d, "key") != "" && (num(d, "expires") == 0 || num(d, "expires") > time.Now().Unix())
		out = append(out, r)
	}
	return out, nil
}
func (a *API) loadIdentity(ctx context.Context, service, name string) (Object, error) {
	a.identity.Lock()
	defer a.identity.Unlock()
	v, e := a.store.Get("identity:" + service)
	if e != nil {
		return nil, e
	}
	if v != nil {
		i := obj(v)
		if name != "" && name != str(i, "name") {
			if _, e = post(ctx, service, str(i, "token"), "/v2/presence", Object{"name": name}); e != nil {
				return nil, e
			}
			i["name"] = name
			if e = a.store.Put("identity:"+service, i); e != nil {
				return nil, e
			}
		}
		return i, nil
	}
	k, e := ecdh.X25519().GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	pub := hex.EncodeToString(k.PublicKey().Bytes())
	r, e := post(ctx, service, "", "/v2/register", Object{"name": name, "public": pub, "isHost": false})
	if e != nil {
		return nil, e
	}
	r["private"] = hex.EncodeToString(k.Bytes())
	r["public"] = pub
	e = a.store.Put("identity:"+service, r)
	return r, e
}
func (a *API) current(id string, g int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.closed && a.generation[id] == g
}
func (a *API) connect(ctx context.Context, p Object) (any, error) {
	service := strings.TrimRight(str(p, "service"), "/")
	name := str(p, "name")
	if name == "" && p["reconnectOnly"] != true {
		name = "Web 浏览器"
	}
	i, e := a.loadIdentity(ctx, service, name)
	if e != nil {
		return nil, e
	}
	token := str(i, "token")
	r, e := post(ctx, service, token, "/v2/resolve", Object{"connectionId": p["code"]})
	if e != nil {
		return nil, e
	}
	d := obj(r["device"])
	id := service + "|" + str(d, "id")
	if str(d, "id") == "" || str(d, "public") == "" {
		return nil, errors.New("无效设备信息")
	}
	a.mu.Lock()
	if a.closed || a.busy[id] {
		a.mu.Unlock()
		return nil, errors.New("正在连接此设备或客户端已关闭")
	}
	a.busy[id] = true
	g := a.generation[id]
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.busy, id); a.mu.Unlock() }()
	if str(p, "public") != "" && p["public"] != d["public"] {
		return nil, errors.New("二维码公钥不匹配")
	}
	saved, e := a.saved()
	if e != nil {
		return nil, e
	}
	s := Object{}
	for _, v := range saved {
		if v["id"] == id {
			s = clone(v)
			break
		}
	}
	expired := num(s, "expires") > 0 && num(s, "expires") <= time.Now().Unix()
	if p["reconnectOnly"] == true && (s["reconnectDisabled"] == true || str(s, "key") == "" || expired) {
		return nil, errors.New("自动重连已停止或授权已过期，请手动连接")
	}
	if str(s, "public") != "" && s["public"] != d["public"] {
		return nil, errors.New("设备身份已变化，需要重新配对")
	}
	if str(s, "key") == "" || expired {
		request := randomHex(16)
		envelope, e := seal(i, str(d, "public"), "agenthub/pair/request/"+request, Object{"password": p["password"], "remember": p["remember"] != false})
		if e != nil {
			return nil, e
		}
		if _, e = post(ctx, service, token, "/v2/pair/request", Object{"id": request, "host": d["id"], "envelope": envelope}); e != nil {
			return nil, e
		}
		until := time.Now().Add(125 * time.Second)
		var result Object
		for time.Now().Before(until) {
			if !a.current(id, g) {
				return nil, errors.New("连接已取消")
			}
			result, e = post(ctx, service, token, "/v2/pair/result", Object{"id": request})
			if e != nil {
				return nil, e
			}
			if result["state"] == "denied" {
				return nil, errors.New("主机拒绝授权或密码错误")
			}
			if result["state"] == "approved" {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		if result["state"] != "approved" {
			return nil, errors.New("等待主机确认超时")
		}
		reply, e := openEnvelope(i, str(d, "public"), "agenthub/pair/reply/"+request, str(result, "reply"))
		if e != nil {
			return nil, e
		}
		if reply["host"] != d["id"] || reply["public"] != d["public"] {
			return nil, errors.New("无效主机授权")
		}
		s = Object{"id": id, "service": service, "connectionId": d["connectionId"], "name": d["name"], "host": d["id"], "public": d["public"], "key": reply["key"], "expires": reply["expires"]}
	}
	policy := Object{}
	if p["relayOnly"] != true {
		short, cancel := context.WithTimeout(ctx, 3*time.Second)
		policy, _ = post(short, service, "", "/v2/network", Object{})
		cancel()
	}
	fresh, err := post(ctx, service, token, "/v2/resolve", Object{"connectionId": str(d, "connectionId")})
	var alternate browserclient.Pairing
	if err == nil {
		if endpoint, err := link.PublicEndpoint(str(obj(fresh["device"]), "tunnelUrl")); err == nil && endpoint != "" {
			alternate = browserclient.Pairing{Relay: strings.Replace(endpoint, "https", "wss", 1), Host: str(d, "id"), Key: str(s, "key"), Token: link.TunnelToken(str(s, "key"), str(d, "id"), str(i, "id")), ICE: browserclient.STUN(policy), RelayOnly: p["relayOnly"] == true}
		}
	}
	discover := func(ctx context.Context) (browserclient.Pairing, error) {
		fresh, err := post(ctx, service, token, "/v2/resolve", Object{"connectionId": str(d, "connectionId")})
		if err != nil {
			return browserclient.Pairing{}, err
		}
		target := obj(fresh["device"])
		if str(target, "id") != str(d, "id") || str(target, "public") != str(d, "public") {
			return browserclient.Pairing{}, errors.New("设备身份已变化")
		}
		endpoint, err := link.PublicEndpoint(str(target, "tunnelUrl"))
		if err != nil || endpoint == "" {
			return browserclient.Pairing{}, err
		}
		return browserclient.Pairing{Relay: strings.Replace(endpoint, "https", "wss", 1), Host: str(d, "id"), Key: str(s, "key"), Token: link.TunnelToken(str(s, "key"), str(d, "id"), str(i, "id")), ICE: browserclient.STUN(policy), RelayOnly: p["relayOnly"] == true}, nil
	}
	c, e := browserclient.ConnectRoutes(ctx, browserclient.Pairing{DiscoverTunnel: discover, Relay: strings.Replace(service, "http", "ws", 1), Host: str(d, "id"), Key: str(s, "key"), Token: token, ICE: browserclient.STUN(policy), RelayOnly: p["relayOnly"] == true}, alternate)
	if e != nil {
		return nil, e
	}
	a.edit.Lock()
	if !a.current(id, g) {
		a.edit.Unlock()
		c.Close()
		return nil, errors.New("连接已取消")
	}
	s["name"], s["connectionId"] = d["name"], d["connectionId"]
	s["relayOnly"] = p["relayOnly"] == true
	s["reconnectDisabled"] = false
	v, e := a.saved()
	if e == nil {
		out := []Object{}
		for _, x := range v {
			if x["id"] != id {
				out = append(out, x)
			}
		}
		e = a.store.Put("devices", append(out, s))
	}
	if e != nil {
		a.edit.Unlock()
		c.Close()
		return nil, e
	}
	a.mu.Lock()
	old := a.clients[id]
	a.clients[id] = c
	a.mu.Unlock()
	a.edit.Unlock()
	if old != nil {
		old.Close()
	}
	return a.list()
}
func (a *API) client(id string) *browserclient.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clients[id]
}
func (a *API) Call(ctx context.Context, method string, p Object, target string, command Object) (any, error) {
	switch method {
	case "client.list":
		return a.list()
	case "client.connect":
		return a.connect(ctx, p)
	case "client.reconnect":
		v, e := a.saved()
		if e != nil {
			return nil, e
		}
		for _, s := range v {
			if s["id"] == p["id"] {
				service := s["service"]
				if str(p, "service") != "" {
					service = p["service"]
				}
				return a.connect(ctx, Object{"service": service, "code": s["connectionId"], "public": s["public"], "relayOnly": s["relayOnly"], "reconnectOnly": true})
			}
		}
		return nil, errors.New("设备配对不存在")
	case "client.disconnect", "client.forget":
		id := str(p, "id")
		a.edit.Lock()
		a.mu.Lock()
		a.generation[id]++
		c := a.clients[id]
		delete(a.clients, id)
		a.mu.Unlock()
		if c != nil {
			c.Close()
		}
		v, e := a.saved()
		if e == nil {
			out := []Object{}
			for _, d := range v {
				if d["id"] == id {
					if method == "client.forget" {
						continue
					}
					d["reconnectDisabled"] = true
				}
				out = append(out, d)
			}
			e = a.store.Put("devices", out)
		}
		a.edit.Unlock()
		if e != nil {
			return nil, e
		}
		if method == "client.forget" {
			_, _ = a.assetCache("client.assetCache.clear", id, nil)
			keys, e := a.store.Keys()
			if e != nil {
				return nil, e
			}
			for _, k := range keys {
				if strings.HasPrefix(k, "directory:"+id+":") || strings.HasPrefix(k, "rpc:"+id+":") {
					_ = a.store.Delete(k)
				}
			}
		}
		return nil, nil
	case "client.call":
		b, _ := json.Marshal(command)
		key := "rpc:" + target + ":" + string(b)
		read := false
		switch str(command, "method") {
		case "providers", "sessions", "models", "transcript", "approvals":
			read = true
		}
		c := a.client(target)
		if c == nil || !c.Connected() {
			if read {
				v, e := a.store.Get(key)
				if e != nil || v != nil {
					return v, e
				}
			}
			return nil, errors.New("设备离线，请重新连接")
		}
		v, e := c.Call(ctx, command)
		if e == nil && read {
			_ = a.store.Put(key, v)
		}
		return v, e
	case "client.sync":
		key := "directory:" + target + ":" + str(p, "provider")
		v, e := a.store.Get(key)
		if e != nil {
			return nil, e
		}
		old := obj(v)
		items := clone(obj(old["items"]))
		c := a.client(target)
		if c != nil && c.Connected() {
			v, e := c.Call(ctx, Object{"method": "sync", "provider": p["provider"], "epoch": old["epoch"], "after": num(old, "cursor")})
			if e != nil {
				return nil, e
			}
			d := obj(v)
			if d["reset"] == true {
				items = Object{}
			}
			for _, x := range arr(d["items"]) {
				items[str(x, "id")] = x
			}
			for _, x := range arr(d["changes"]) {
				if x["deleted"] == true {
					delete(items, str(x, "id"))
				} else {
					items[str(x, "id")] = x["value"]
				}
			}
			if e = a.store.Put(key, Object{"items": items, "cursor": d["cursor"], "epoch": d["epoch"]}); e != nil {
				return nil, e
			}
		}
		out := []Object{}
		for _, x := range items {
			out = append(out, obj(x))
		}
		return out, nil
	case "client.assetCache.get", "client.assetCache.put", "client.assetCache.clear":
		return a.assetCache(method, target, p)
	default:
		return nil, errors.New("浏览器仅提供远控功能")
	}
}
func (a *API) assetCache(method, target string, p Object) (any, error) {
	a.assets.Lock()
	defer a.assets.Unlock()
	b, _ := json.Marshal([]string{target, str(p, "key")})
	key := "asset:" + string(b)
	v, e := a.store.Get("asset-index")
	if e != nil {
		return nil, e
	}
	index := arr(v)
	if method == "client.assetCache.get" {
		v, e := a.store.Get(key)
		if e == nil && v != nil {
			for _, x := range index {
				if x["key"] == key {
					x["touched"] = time.Now().UnixMilli()
				}
			}
			_ = a.store.Put("asset-index", index)
		}
		return v, e
	}
	if method == "client.assetCache.clear" {
		out := []Object{}
		for _, x := range index {
			if target == "" || x["device"] == target {
				_ = a.store.Delete(str(x, "key"))
			} else {
				out = append(out, x)
			}
		}
		return nil, a.store.Put("asset-index", out)
	}
	bytes, e := base64.StdEncoding.DecodeString(str(p, "data"))
	if e != nil || len(bytes) > 50<<20 {
		return nil, errors.New("附件超过缓存上限或格式无效")
	}
	out := []Object{}
	var size int64
	for _, x := range index {
		if x["key"] != key {
			out = append(out, x)
			size += num(x, "size")
		}
	}
	for size+int64(len(bytes)) > 96<<20 && len(out) > 0 {
		old := 0
		for i, x := range out {
			if num(x, "touched") < num(out[old], "touched") {
				old = i
			}
		}
		_ = a.store.Delete(str(out[old], "key"))
		size -= num(out[old], "size")
		out = append(out[:old], out[old+1:]...)
	}
	if e = a.store.Put(key, Object{"data": p["data"]}); e != nil {
		return nil, e
	}
	return nil, a.store.Put("asset-index", append(out, Object{"key": key, "device": target, "size": len(bytes), "touched": time.Now().UnixMilli()}))
}
