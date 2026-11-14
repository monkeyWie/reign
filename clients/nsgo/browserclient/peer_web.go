//go:build js && wasm

package browserclient

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"
)

func pingSocket(ctx context.Context, ws *websocket.Conn) (bool, error) {
	return false, ws.Write(ctx, websocket.MessageText, []byte("reign:ping"))
}

type jsPeer struct {
	client         *Client
	pc, dc         js.Value
	callbacks      []js.Func
	direct, closed atomic.Bool
	mu             sync.Mutex
	remote         bool
	candidates     []js.Value
}

func Await(p js.Value) (js.Value, error) {
	type result struct {
		v js.Value
		e error
	}
	ch := make(chan result, 1)
	ok := js.FuncOf(func(_ js.Value, args []js.Value) any {
		v := js.Undefined()
		if len(args) > 0 {
			v = args[0]
		}
		ch <- result{v: v}
		return nil
	})
	bad := js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "浏览器操作失败"
		if len(args) > 0 {
			message = args[0].String()
		}
		ch <- result{e: errors.New(message)}
		return nil
	})
	p.Call("then", ok, bad)
	r := <-ch
	ok.Release()
	bad.Release()
	return r.v, r.e
}
func jsObject(v Object) js.Value {
	b, _ := jsonMarshal(v)
	return js.Global().Get("JSON").Call("parse", string(b))
}
func newPeer(c *Client, urls []string) (result peer, err error) {
	// A browser may expose the constructor but reject it in this context.
	// Keep the working WS relay instead of crashing the WASM client.
	defer func() {
		if recover() != nil {
			result = nil
			err = errors.New("当前环境无法创建 WebRTC，继续使用中继")
		}
	}()
	ctor := js.Global().Get("RTCPeerConnection")
	if ctor.Type() != js.TypeFunction {
		return nil, errors.New("当前环境无法使用 WebRTC")
	}
	servers := []any{}
	for _, u := range urls {
		servers = append(servers, map[string]any{"urls": []any{u}})
	}
	p := &jsPeer{client: c, pc: ctor.New(map[string]any{"iceServers": servers})}
	p.dc = p.pc.Call("createDataChannel", "reign.p2p", map[string]any{"ordered": true})
	p.dc.Set("binaryType", "arraybuffer")
	p.bind(p.pc, "onicecandidate", func(e js.Value) {
		candidate := e.Get("candidate")
		if candidate.Truthy() {
			c.note("candidate:"+candidate.Get("candidate").String(), "udp", "发现 WebRTC 直连候选", candidate.Get("candidate").String(), "info")
			var v Object
			_ = jsonUnmarshal([]byte(js.Global().Get("JSON").Call("stringify", candidate.Call("toJSON")).String()), &v)
			go func() { _ = c.signal(Object{"type": "candidate", "candidate": v}) }()
		}
	})
	p.bind(p.pc, "onconnectionstatechange", func(js.Value) {
		state := p.pc.Get("connectionState").String()
		if state == "connecting" {
			c.note("ice-check", "udp", "正在验证 WebRTC 直连路径", "", "running")
		}
		if state == "connected" && p.dc.Get("readyState").String() == "open" {
			p.direct.Store(true)
			c.note("path:webrtc", "direct", "WebRTC 直连已恢复", "复用已有加密数据通道", "success")
		}
		if state == "failed" || state == "disconnected" || state == "closed" {
			p.direct.Store(false)
			c.note("ice-check", "udp", "WebRTC 直连暂不可用", "connectionState="+state+" · iceConnectionState="+p.pc.Get("iceConnectionState").String(), "warning")
		}
	})
	p.bind(p.dc, "onopen", func(js.Value) {
		p.direct.Store(true)
		c.note("path:webrtc", "direct", "已建立 WebRTC 直连", "端到端加密数据通道", "success")
	})
	p.bind(p.dc, "onclose", func(js.Value) { p.direct.Store(false) })
	p.bind(p.dc, "onerror", func(js.Value) { p.direct.Store(false) })
	p.bind(p.dc, "onmessage", func(e js.Value) {
		raw := js.Global().Get("Uint8Array").New(e.Get("data"))
		if raw.Get("byteLength").Int() > 65536 {
			return
		}
		b := make([]byte, raw.Get("byteLength").Int())
		js.CopyBytesToGo(b, raw)
		go func() { _ = c.receiveChunk(b) }()
	})
	return p, nil
}
func (p *jsPeer) bind(v js.Value, key string, fn func(js.Value)) {
	f := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !p.closed.Load() {
			e := js.Undefined()
			if len(args) > 0 {
				e = args[0]
			}
			fn(e)
		}
		return nil
	})
	p.callbacks = append(p.callbacks, f)
	v.Set(key, f)
}
func (p *jsPeer) Offer() error {
	offer, e := Await(p.pc.Call("createOffer"))
	if e != nil {
		return e
	}
	if _, e = Await(p.pc.Call("setLocalDescription", offer)); e != nil {
		return e
	}
	local := p.pc.Get("localDescription")
	return p.client.signal(Object{"type": "offer", "sdp": local.Get("sdp").String()})
}
func (p *jsPeer) Signal(v Object) error {
	if p.closed.Load() {
		return errors.New("WebRTC 已关闭")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch v["type"] {
	case "answer":
		if _, e := Await(p.pc.Call("setRemoteDescription", jsObject(v))); e != nil {
			return e
		}
		p.remote = true
		for _, candidate := range p.candidates {
			_, _ = Await(p.pc.Call("addIceCandidate", candidate))
		}
		p.candidates = nil
	case "candidate":
		raw, ok := v["candidate"].(map[string]any)
		if !ok {
			return errors.New("无效 ICE candidate")
		}
		candidate := jsObject(raw)
		if p.remote {
			_, e := Await(p.pc.Call("addIceCandidate", candidate))
			return e
		}
		if len(p.candidates) < 256 {
			p.candidates = append(p.candidates, candidate)
		}
	}
	return nil
}
func (p *jsPeer) Recovering() bool {
	return !p.closed.Load() && p.dc.Get("readyState").String() == "open" && p.pc.Get("connectionState").String() == "disconnected"
}
func (p *jsPeer) Direct() bool { return p.direct.Load() && !p.closed.Load() }
func (p *jsPeer) Send(b []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			p.direct.Store(false)
			err = errors.New("WebRTC 发送失败")
		}
	}()
	if !p.Direct() {
		return errors.New("WebRTC 已断开")
	}
	deadline := time.Now().Add(8 * time.Second)
	for p.dc.Get("bufferedAmount").Int() > 512<<10 {
		if !p.Direct() || time.Now().After(deadline) {
			return errors.New("WebRTC 发送超时，结果尚未确认")
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(raw, b)
	p.dc.Call("send", raw)
	return nil
}
func (p *jsPeer) Close() {
	if p.closed.Swap(true) {
		return
	}
	p.direct.Store(false)
	for _, k := range []string{"onicecandidate", "onconnectionstatechange"} {
		p.pc.Set(k, js.Null())
	}
	for _, k := range []string{"onopen", "onclose", "onerror", "onmessage"} {
		p.dc.Set(k, js.Null())
	}
	p.dc.Call("close")
	p.pc.Call("close")
	for _, f := range p.callbacks {
		f.Release()
	}
}
