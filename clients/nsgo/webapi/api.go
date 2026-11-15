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
	"fmt"
	"github.com/monkeyWie/reignhub/clients/nsgo/browserclient"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/device"
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
			if _, e = post(ctx, service, str(i, "token"), "/p2p/devices/presence", Object{"name": name}); e != nil {
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
	r, e := post(ctx, service, "", "/p2p/devices/register", Object{"name": name, "public": pub, "isHost": false})
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
	connectivity.Report(ctx, "service", "正在连接服务并验证本机身份", connectivity.StageTimeout)
	i, e := a.loadIdentity(ctx, service, name)
	if e != nil {
		return nil, e
	}
	token := str(i, "token")
	connectivity.Note(ctx, "service", "service", "本机身份已验证", service, "success")
	var r Object
	cached := obj(p["cached"])
	if len(cached) > 0 {
		r = Object{"online": true, "device": Object{"id": cached["host"], "public": cached["public"], "name": cached["name"], "connectionId": cached["connectionId"]}}
	} else {
		connectivity.Report(ctx, "lookup", "正在查找被控设备", connectivity.StageTimeout)
		r, e = post(ctx, service, token, "/p2p/devices/resolve", Object{"connectionId": p["code"]})
		if e != nil {
			return nil, e
		}
	}
	d := obj(r["device"])
	if r["online"] == false {
		return nil, errors.New("被控设备离线，请确认电脑上的 ReignHub 正在运行并已连接服务")
	}
	connectivity.Note(ctx, "lookup", "lookup", "已找到被控设备", str(d, "name"), "success")
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
		connectivity.Report(ctx, "authorize", "等待被控设备授权", connectivity.StageTimeout)
		request := randomHex(16)
		envelope, e := seal(i, str(d, "public"), "reignhub/pair/request/"+request, Object{"password": p["password"], "remember": p["remember"] != false})
		if e != nil {
			return nil, e
		}
		if _, e = post(ctx, service, token, "/p2p/pairings/request", Object{"id": request, "host": d["id"], "envelope": envelope}); e != nil {
			return nil, e
		}
		approvalCtx, stopApproval := context.WithTimeout(ctx, connectivity.StageTimeout)
		defer stopApproval()
		var result Object
		for approvalCtx.Err() == nil {
			if !a.current(id, g) {
				return nil, errors.New("连接已取消")
			}
			result, e = post(approvalCtx, service, token, "/p2p/pairings/result", Object{"id": request})
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
			case <-approvalCtx.Done():
				return nil, fmt.Errorf("等待主机确认超时或取消: %w", approvalCtx.Err())
			case <-time.After(time.Second):
			}
		}
		if result["state"] != "approved" {
			return nil, errors.New("等待主机确认超时")
		}
		reply, e := openEnvelope(i, str(d, "public"), "reignhub/pair/reply/"+request, str(result, "reply"))
		if e != nil {
			return nil, e
		}
		if reply["host"] != d["id"] || reply["public"] != d["public"] {
			return nil, errors.New("无效主机授权")
		}
		s = Object{"id": id, "service": service, "connectionId": d["connectionId"], "name": d["name"], "host": d["id"], "public": d["public"], "key": reply["key"], "expires": reply["expires"]}
	}
	connectivity.Note(ctx, "authorize", "authorize", "设备授权已就绪", "", "success")
	policy := Object{}
	if p["relayOnly"] != true {
		connectivity.Report(ctx, "config", "正在获取直连探测配置", connectivity.StageTimeout)
		entry, _ := a.store.Get("network:" + service)
		cache := obj(entry)
		if len(cached) > 0 && num(cache, "at") > time.Now().Add(-10*time.Minute).Unix() {
			policy = obj(cache["policy"])
		} else {
			policy, e = post(ctx, service, "", "/p2p/config", Object{})
			if e == nil {
				_ = a.store.Put("network:"+service, Object{"policy": policy, "at": time.Now().Unix()})
			}
		}
		if e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			connectivity.Note(ctx, "config", "config", "探测配置暂不可用，使用公共 STUN", e.Error(), "warning")
		} else {
			connectivity.Note(ctx, "config", "config", "直连探测配置已获取", strings.Join(browserclient.STUN(policy), ", "), "success")
		}
	}
	c, e := browserclient.Connect(ctx, browserclient.Pairing{Relay: strings.Replace(service, "http", "ws", 1), Host: str(d, "id"), Key: str(s, "key"), Token: token, ICE: browserclient.STUN(policy), RelayOnly: p["relayOnly"] == true})
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
	if strings.HasPrefix(method, "client.account.") {
		paths := map[string]string{"start": "/api/account/device-login/start", "poll": "/api/account/device-login/poll", "status": "/api/account/device-status", "devices": "/api/account/device-list"}
		path, ok := paths[strings.TrimPrefix(method, "client.account.")]
		if !ok {
			return nil, errors.New("无效账号操作")
		}
		service := strings.TrimRight(str(p, "service"), "/")
		i, err := a.loadIdentity(ctx, service, str(p, "name"))
		if err != nil {
			return nil, err
		}
		return post(ctx, service, str(i, "token"), path, Object{"id": p["id"]})
	}
	switch method {
	case "client.list":
		return a.list()
	case "client.cached":
		return a.cached(target, command)
	case "client.snapshot":
		if target == "" || str(p, "provider") == "" || str(p, "session") == "" || p["transcript"] == nil {
			return nil, errors.New("invalid local transcript snapshot")
		}
		return nil, a.store.Put(snapshotKey(target, str(p, "provider"), str(p, "session")), p["transcript"])
	case "client.presence":
		saved, err := a.saved()
		if err != nil {
			return nil, err
		}
		targets := []device.PresenceTarget{}
		for _, s := range saved {
			identity, _ := a.store.Get("identity:" + str(s, "service"))
			targets = append(targets, device.PresenceTarget{ID: str(s, "id"), Service: str(s, "service"), Host: str(s, "host"), Token: str(obj(identity), "token")})
		}
		out := []Object{}
		for _, status := range device.Presence(ctx, targets) {
			item := Object{"id": status.ID, "host": status.Host, "name": status.Name, "checkedAt": status.CheckedAt, "online": nil}
			if status.Online != nil {
				item["online"] = *status.Online
			}
			out = append(out, item)
		}
		return out, ctx.Err()
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
				if s["reconnectDisabled"] == true || str(s, "key") == "" || (num(s, "expires") > 0 && num(s, "expires") <= time.Now().Unix()) {
					return nil, errors.New("自动重连已停止或授权已过期，请手动连接")
				}
				if old := a.client(str(s, "id")); old != nil {
					connectivity.Report(ctx, "resume", "正在恢复原有连接", connectivity.StageTimeout)
					probe, stop := context.WithTimeout(ctx, 8*time.Second)
					err := old.Resume(probe)
					stop()
					if err == nil {
						return a.list()
					}
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					old.Close()
				}
				params := Object{"service": service, "code": s["connectionId"], "public": s["public"], "relayOnly": s["relayOnly"], "reconnectOnly": true, "cached": s}
				connectivity.Report(ctx, "resume", "正在复用设备配对与连接配置", connectivity.StageTimeout)
				result, err := a.connect(ctx, params)
				if err == nil || ctx.Err() != nil {
					return result, err
				}
				connectivity.Note(ctx, "resume-fallback", "retry", "快速恢复未成功，重新查找设备", err.Error(), "warning")
				delete(params, "cached")
				return a.connect(ctx, params)
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
				if strings.HasPrefix(k, "directory:"+id+":") || strings.HasPrefix(k, "rpc:"+id+":") || strings.HasPrefix(k, "snapshot:"+id+":") {
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
