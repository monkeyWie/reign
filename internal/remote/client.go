package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/relay"
)

type Client struct {
	wire       *wire
	mu         sync.Mutex
	pending    map[string]chan control.Response
	done       chan struct{}
	cache      map[string]snapshot
	cacheBytes int
}

func Connect(ctx context.Context, p Pairing, opts Options) (*Client, error) {
	ws, dataWS, peer, err := dial(ctx, p, "client")
	if err != nil {
		return nil, err
	}
	c := &Client{pending: make(map[string]chan control.Response), done: make(chan struct{})}
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
	go drainRelay(w.ctx, dataWS)
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
	if err = w.handshake(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("配对握手失败: %w", err)
	}
	go w.offer()
	// Prove possession of the pairing key before presenting a connected UI.
	var statuses json.RawMessage
	if err = c.Call(ctx, control.Request{Method: "providers"}, &statuses); err != nil {
		c.Close()
		return nil, fmt.Errorf("配对验证失败: %w", err)
	}
	return c, nil
}
func (c *Client) Path() string {
	select {
	case <-c.done:
		return "Disconnected"
	default:
		return c.wire.path()
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
	if err = c.wire.send(raw); err != nil {
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
