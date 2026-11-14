package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/relay"
)

type Client struct {
	wire       *wire
	mu         sync.Mutex
	pending    map[string]chan control.Response
	done       chan struct{}
	relayDone  chan struct{}
	cache      map[string]snapshot
	cacheBytes int
}

func Connect(ctx context.Context, p Pairing, opts Options) (*Client, error) {
	if opts.Progress == nil {
		opts.Progress = connectivity.Observer(ctx)
	} else {
		ctx = connectivity.WithProgress(ctx, opts.Progress)
	}
	ws, dataWS, peer, err := dialWithRelayPolicy(ctx, p, "client", connectivity.StageTimeout, opts.RelayOnly)
	if err != nil {
		return nil, err
	}
	c := &Client{pending: make(map[string]chan control.Response), done: make(chan struct{}), relayDone: make(chan struct{})}
	w, err := newWire(context.Background(), ws, peer, p.Key, false, opts, func(data []byte) {
		var r control.Response
		if json.Unmarshal(data, &r) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[r.ID]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- r:
			default:
			}
		}
	})
	if err != nil {
		ws.CloseNow()
		return nil, err
	}
	c.wire = w
	w.dataWS = dataWS
	go func() { defer close(c.relayDone); drainRelay(w.ctx, dataWS) }()
	go keepAlive(w.ctx, ws)
	go func() {
		defer close(c.done)
		defer w.close()
		defer ws.CloseNow()
		for {
			_, raw, err := ws.Read(w.ctx)
			if err != nil {
				return
			}
			var f relay.Frame
			if json.Unmarshal(raw, &f) == nil {
				w.receive(f)
			}
		}
	}()
	connectivity.Report(ctx, "handshake", "正在与被控电脑建立加密握手", connectivity.StageTimeout)
	handshakeCtx, stopHandshake := context.WithTimeout(ctx, connectivity.StageTimeout)
	err = w.handshake(handshakeCtx)
	stopHandshake()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("配对握手失败: %w", err)
	}
	connectivity.Note(ctx, "handshake", "handshake", "端到端加密握手完成", "已验证设备配对密钥", "success")
	message := "正在尝试 P2P 直连；暂无可用的备用中继通道"
	if dataWS != nil {
		message = "优先尝试 P2P 直连，必要时自动使用中继"
	}
	if opts.RelayOnly {
		if dataWS == nil {
			c.Close()
			return nil, errors.New("当前服务未提供中继权限，请关闭“仅用中继”后尝试直连，或使用支持中继的服务")
		}
		message = "正在验证中继连接"
	}
	connectivity.Report(ctx, "direct", message, connectivity.StageTimeout)
	go w.offer()
	if err = w.waitPath(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("连接路径验证失败: %w", err)
	}
	connectivity.Report(ctx, "verify", "正在验证设备响应", connectivity.StageTimeout)
	verifyCtx, stopVerify := context.WithTimeout(ctx, connectivity.StageTimeout)
	defer stopVerify()
	// Prove possession of the pairing key before presenting a connected UI.
	var statuses json.RawMessage
	if err = c.Call(verifyCtx, control.Request{Method: "providers"}, &statuses); err != nil {
		c.Close()
		return nil, fmt.Errorf("配对验证失败: %w", err)
	}
	go c.watchPaths()
	return c, nil
}

// Business RPCs have no total deadline. End their wait when every usable data
// path has actually gone away, even if the signaling socket remains open.
func (c *Client) watchPaths() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-tick.C:
			if c.Path() == "Disconnected" {
				_ = c.Close()
				return
			}
		}
	}
}
func (c *Client) Path() string {
	select {
	case <-c.done:
		return "Disconnected"
	default:
	}
	path := c.wire.path()
	if strings.HasPrefix(path, "P2P") {
		return path
	}
	select {
	case <-c.relayDone:
		return "Disconnected"
	default:
		return path
	}
}
func (c *Client) Close() error {
	if c.wire.dataWS != nil {
		c.wire.dataWS.CloseNow()
	}
	c.wire.close()
	return c.wire.ws.CloseNow()
}
func (c *Client) Call(ctx context.Context, r control.Request, out any) error {
	cacheKey := ""
	var cached snapshot
	if control.ReadOnly(r.Method) {
		r.ID = ""
		r.Revision = ""
		encoded, _ := json.Marshal(r)
		cacheKey = string(encoded)
		c.mu.Lock()
		cached = c.cache[cacheKey]
		c.mu.Unlock()
		r.Revision = cached.revision
	}
	if r.ID == "" {
		r.ID = fmt.Sprintf("%s-%d", relay.ID(), nextRequest.Add(1))
	}
	ch := make(chan control.Response, 1)
	c.mu.Lock()
	c.pending[r.ID] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, r.ID); c.mu.Unlock() }()
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("request too large")
	}
	if err = c.wire.sendContext(ctx, raw); err != nil {
		return err
	}
	select {
	case result := <-ch:
		if result.Error != "" {
			return errors.New(result.Error)
		}
		if result.NotModified {
			if cacheKey == "" || cached.revision == "" || result.Revision != cached.revision {
				return errors.New("invalid conditional response")
			}
			result.Result = cached.data
		} else if cacheKey != "" && result.Revision != "" {
			c.remember(cacheKey, snapshot{revision: result.Revision, data: result.Result})
		}
		if out != nil {
			return json.Unmarshal(result.Result, out)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("请求超时或取消；操作结果可能尚未返回，请刷新后再决定是否重试: %w", ctx.Err())
	case <-c.done:
		return errors.New("relay 已断开；请重新连接，刷新后再重试操作")
	}
}

type snapshot struct {
	revision string
	data     json.RawMessage
}

func (c *Client) remember(key string, entry snapshot) {
	const limit = 16 << 20
	if len(entry.data) > limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = make(map[string]snapshot)
	}
	if previous, ok := c.cache[key]; ok {
		c.cacheBytes -= len(previous.data)
		delete(c.cache, key)
	}
	for len(c.cache) >= 32 || c.cacheBytes+len(entry.data) > limit {
		for k, v := range c.cache {
			c.cacheBytes -= len(v.data)
			delete(c.cache, k)
			break
		}
	}
	c.cache[key] = entry
	c.cacheBytes += len(entry.data)
}
