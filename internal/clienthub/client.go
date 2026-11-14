// Package clienthub is the shared native client used by desktop and mobile bridges.
package clienthub

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/assets"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/remote"
	"github.com/monkeyWie/reignhub/internal/store"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Saved struct {
	ID                string `json:"id"`
	Service           string `json:"service"`
	ConnectionID      string `json:"connectionId"`
	Name              string `json:"name"`
	Host              string `json:"host"`
	Public            string `json:"public"`
	Key               string `json:"key"`
	Expires           int64  `json:"expires"`
	RelayOnly         bool   `json:"relayOnly,omitempty"`
	ReconnectDisabled bool   `json:"reconnectDisabled,omitempty"`
}
type Hub struct {
	Assets      *assets.Cache
	mu          sync.Mutex
	identityMu  sync.Mutex
	DB          *store.DB
	connections map[string]*remote.Client
	busy        map[string]bool
	generation  map[string]uint64
}

func New(dir string) (*Hub, error) {
	db, e := store.Open(filepath.Join(dir, "client.db"))
	if e != nil {
		return nil, e
	}
	return &Hub{DB: db, Assets: &assets.Cache{Root: filepath.Join(dir, "asset-cache")}, connections: map[string]*remote.Client{}, busy: map[string]bool{}, generation: map[string]uint64{}}, nil
}

// Close is called after the application's outstanding requests are canceled.
func (h *Hub) Close() error {
	h.mu.Lock()
	connections := h.connections
	h.connections = map[string]*remote.Client{}
	h.mu.Unlock()
	for _, c := range connections {
		_ = c.Close()
	}
	return h.DB.Close()
}
func (h *Hub) identity(ctx context.Context, service, name string) (device.Identity, error) {
	h.identityMu.Lock()
	defer h.identityMu.Unlock()
	var i device.Identity
	k := "identity:" + service
	if e := h.DB.Get(k, &i); e == nil {
		if name != "" && name != i.Name {
			if e := device.Post(ctx, service, i.Token, "/p2p/devices/presence", map[string]any{"name": name}, nil); e != nil {
				return i, e
			}
			i.Name = name
			if e := h.DB.Put(k, i); e != nil {
				return i, e
			}
		}
		return i, nil
	}
	i, e := device.NewIdentity(name)
	if e != nil {
		return i, e
	}
	i, e = device.Register(ctx, service, i, false)
	if e != nil {
		return i, e
	}
	return i, h.DB.Put(k, i)
}
func (h *Hub) List() ([]map[string]any, error) {
	keys, e := h.DB.Keys("saved:")
	if e != nil {
		return nil, e
	}
	r := []map[string]any{}
	for _, k := range keys {
		var s Saved
		if e = h.DB.Get(k, &s); e != nil {
			return nil, e
		}
		path := "离线"
		h.mu.Lock()
		c := h.connections[s.ID]
		h.mu.Unlock()
		if c != nil {
			path = c.Path()
		}
		connected := c != nil && path != "Disconnected"
		r = append(r, map[string]any{"id": s.ID, "service": s.Service, "connectionId": s.ConnectionID, "name": s.Name, "host": s.Host, "public": s.Public, "path": path, "connected": connected, "autoReconnect": !s.ReconnectDisabled && s.Key != "" && (s.Expires == 0 || s.Expires > time.Now().Unix())})
	}
	return r, nil
}
func (h *Hub) Connect(ctx context.Context, service, code, public, password, name string, remember, relayOnly bool) (Saved, error) {
	return h.connect(ctx, service, code, public, password, name, remember, relayOnly, false)
}

// Reconnect only reuses an existing valid grant. It never requests new approval
// or replays a user command after a network failure.
func (h *Hub) Reconnect(ctx context.Context, id string) (Saved, error) {
	var s Saved
	if err := h.DB.Get("saved:"+id, &s); err != nil {
		return s, err
	}
	return h.connect(ctx, s.Service, s.ConnectionID, s.Public, "", "", true, s.RelayOnly, true)
}

func (h *Hub) connect(ctx context.Context, service, code, public, password, name string, remember, relayOnly, reconnectOnly bool) (Saved, error) {
	ctx, cancelAttempt := context.WithTimeout(ctx, 190*time.Second)
	defer cancelAttempt()
	service, e := device.ServiceURL(service)
	if e != nil {
		return Saved{}, e
	}
	connectivity.Report(ctx, "service", "正在连接服务并验证本机身份", 15*time.Second)
	i, e := h.identity(ctx, service, name)
	if e != nil {
		return Saved{}, e
	}
	var r struct {
		Device deviceservice.Device
		Online bool
	}
	connectivity.Report(ctx, "resolve", "正在查找设备并检查在线状态", 12*time.Second)
	if e = device.Post(ctx, service, i.Token, "/p2p/devices/resolve", map[string]any{"connectionId": code}, &r); e != nil {
		return Saved{}, e
	}
	if !r.Online {
		return Saved{}, fmt.Errorf("被控设备离线，请在电脑上打开 ReignHub，并确认两端使用相同的服务地址")
	}
	d := r.Device
	if public != "" && public != d.Public {
		return Saved{}, fmt.Errorf("二维码公钥不匹配")
	}
	id := service + "|" + d.ID
	h.mu.Lock()
	if h.busy[id] {
		h.mu.Unlock()
		return Saved{}, fmt.Errorf("正在连接该设备")
	}
	h.busy[id] = true
	generation := h.generation[id]
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.busy, id); h.mu.Unlock() }()
	var s Saved
	_ = h.DB.Get("saved:"+id, &s)
	if reconnectOnly && (s.ReconnectDisabled || s.Key == "" || (s.Expires > 0 && s.Expires <= time.Now().Unix())) {
		return s, fmt.Errorf("自动重连已停止或授权已过期，请手动连接")
	}
	if s.Public != "" && s.Public != d.Public {
		return Saved{}, fmt.Errorf("设备身份已变化，需要删除旧配对再确认")
	}
	if s.Key == "" || (s.Expires > 0 && s.Expires <= time.Now().Unix()) {
		connectivity.Report(ctx, "request", "正在向被控电脑请求授权", 12*time.Second)
		reqID := device.Random()[:32]
		envelope, e := device.Seal(i.Private, d.Public, "reignhub/pair/request/"+reqID, map[string]any{"password": password, "remember": remember})
		if e != nil {
			return s, e
		}
		if e = device.Post(ctx, service, i.Token, "/p2p/pairings/request", map[string]any{"host": d.ID, "id": reqID, "envelope": envelope}, nil); e != nil {
			return s, e
		}
		message := "请在被控电脑的 ReignHub 中允许此次连接"
		if password != "" {
			message = "正在等待被控电脑验证访问密码"
		}
		connectivity.Report(ctx, "approval", message, 125*time.Second)
		deadline := time.NewTimer(125 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			var result deviceservice.Pair
			if e = device.Post(ctx, service, i.Token, "/p2p/pairings/result", map[string]string{"id": reqID}, &result); e != nil {
				return s, e
			}
			if result.State == "denied" {
				return s, fmt.Errorf("主机拒绝授权或密码错误")
			}
			if result.State == "approved" {
				var reply struct {
					Key, Host, Public string
					Expires           int64
				}
				if e = device.Open(i.Private, d.Public, "reignhub/pair/reply/"+reqID, result.Reply, &reply); e != nil {
					return s, e
				}
				if reply.Host != d.ID || reply.Public != d.Public {
					return s, fmt.Errorf("无效主机授权")
				}
				s = Saved{ID: id, Service: service, ConnectionID: d.ConnectionID, Name: d.Name, Host: d.ID, Public: d.Public, Key: reply.Key, Expires: reply.Expires}
				if e = h.DB.Put("saved:"+id, s); e != nil {
					return s, e
				}
				break
			}
			select {
			case <-ctx.Done():
				return s, ctx.Err()
			case <-deadline.C:
				return s, fmt.Errorf("等待主机授权超时，请在被控电脑上允许连接后重试")
			case <-tick.C:
			}
		}
	}
	connectCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	opts := remote.Options{RelayOnly: relayOnly}
	if !relayOnly {
		connectivity.Report(ctx, "network", "授权已通过，正在获取直连配置", 3*time.Second)
		policy := device.Network(connectCtx, service)
		opts.ICEServers = policy.Servers()
		opts.TCPSTUN = policy.TCPSTUN
	}
	c, e := remote.Connect(connectCtx, remote.Pairing{Relay: "ws" + strings.TrimPrefix(service, "http"), Host: d.ID, Key: s.Key, RelayToken: i.Token}, opts)
	if e != nil {
		return s, e
	}
	if ctx.Err() != nil {
		_ = c.Close()
		return s, ctx.Err()
	}
	h.mu.Lock()
	if h.generation[id] != generation {
		h.mu.Unlock()
		_ = c.Close()
		return s, fmt.Errorf("连接已取消")
	}
	s.Name, s.ConnectionID = d.Name, d.ConnectionID
	s.RelayOnly, s.ReconnectDisabled = relayOnly, false
	if err := h.DB.Put("saved:"+id, s); err != nil {
		h.mu.Unlock()
		_ = c.Close()
		return s, err
	}
	old := h.connections[id]
	h.connections[id] = c
	h.mu.Unlock()
	if old != nil {
		old.Close()
	}
	connectivity.Report(ctx, "connected", "连接成功", 0)
	return s, nil
}
func (h *Hub) Disconnect(id string) error {
	h.mu.Lock()
	h.generation[id]++
	c := h.connections[id]
	delete(h.connections, id)
	defer h.mu.Unlock()
	if c != nil {
		defer c.Close()
	}
	var s Saved
	if h.DB.Get("saved:"+id, &s) == nil {
		s.ReconnectDisabled = true
		if err := h.DB.Put("saved:"+id, s); err != nil {
			return err
		}
	}
	return nil
}
func (h *Hub) Forget(id string) error {
	_ = h.Assets.Clear(id)
	h.Disconnect(id)
	keys, e := h.DB.Keys("rpc:" + id + ":")
	if e != nil {
		return e
	}
	directories, e := h.DB.Keys("directory:" + id + ":")
	if e != nil {
		return e
	}
	keys = append(keys, directories...)
	for _, k := range keys {
		if e = h.DB.Delete(k); e != nil {
			return e
		}
	}
	return h.DB.Delete("saved:" + id)
}
func (h *Hub) Call(ctx context.Context, id string, r control.Request) (any, error) {
	raw, _ := json.Marshal(r)
	cacheKey := "rpc:" + id + ":" + string(raw)
	h.mu.Lock()
	c := h.connections[id]
	h.mu.Unlock()
	if c == nil || c.Path() == "Disconnected" {
		if control.ReadOnly(r.Method) {
			var data any
			if h.DB.Get(cacheKey, &data) == nil {
				return data, nil
			}
		}
		return nil, fmt.Errorf("设备离线：可以查看缓存，写操作需重新连接")
	}
	var out any
	if e := c.Call(ctx, r, &out); e != nil {
		return nil, e
	}
	if control.ReadOnly(r.Method) {
		if e := h.DB.Put(cacheKey, out); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (h *Hub) ApplyDirectory(id, provider string, delta store.Delta) ([]map[string]any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := "directory:" + id + ":" + provider
	var data struct {
		Items  map[string]map[string]any `json:"items"`
		Epoch  string                    `json:"epoch"`
		Cursor int64                     `json:"cursor"`
	}
	_ = h.DB.Get(k, &data)
	if data.Items == nil || delta.Reset {
		data.Items = map[string]map[string]any{}
	}
	for _, b := range delta.Items {
		var v map[string]any
		if e := json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
		data.Items[v["id"].(string)] = v
	}
	for _, c := range delta.Changes {
		if c.Deleted {
			delete(data.Items, c.ID)
		} else {
			var v map[string]any
			if e := json.Unmarshal(c.Value, &v); e != nil {
				return nil, e
			}
			data.Items[c.ID] = v
		}
	}
	data.Epoch = delta.Epoch
	data.Cursor = delta.Cursor
	if e := h.DB.Put(k, data); e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for _, v := range data.Items {
		out = append(out, v)
	}
	return out, nil
}
func (h *Hub) Sync(ctx context.Context, id, p string) (any, error) {
	var state struct {
		Epoch  string
		Cursor int64
		Items  map[string]map[string]any
	}
	_ = h.DB.Get("directory:"+id+":"+p, &state)
	h.mu.Lock()
	c := h.connections[id]
	h.mu.Unlock()
	if c == nil || c.Path() == "Disconnected" {
		out := []map[string]any{}
		for _, v := range state.Items {
			out = append(out, v)
		}
		return out, nil
	}
	var delta store.Delta
	if e := c.Call(ctx, control.Request{Method: "sync", Provider: p, Epoch: state.Epoch, After: state.Cursor}, &delta); e != nil {
		return nil, e
	}
	return h.ApplyDirectory(id, p, delta)
}
