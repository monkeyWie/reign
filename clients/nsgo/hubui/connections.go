package hubui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"
)

// Connection health belongs to a device, not to the last user operation. Keep
// independent read failures until that read succeeds; a heartbeat must not
// hide a failed catalogue or transcript refresh.
type connectionHealth struct {
	attempt      *connectAttempt
	issues       map[string]string
	reconnecting bool
	established  bool
}

func (s *State) connection(id string) *connectionHealth {
	if s.connections == nil {
		s.connections = map[string]*connectionHealth{}
	}
	if s.connections[id] == nil {
		s.connections[id] = &connectionHealth{issues: map[string]string{}}
	}
	return s.connections[id]
}
func transportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	raw := strings.ToLower(err.Error())
	for _, text := range []string{"deadline exceeded", "timeout", "timed out", "device offline", "disconnected", "connection reset", "connection refused", "network is unreachable", "network is down", "broken pipe", "websocket", "设备离线", "relay 已断开", "连接已断开"} {
		if strings.Contains(raw, text) {
			return true
		}
	}
	return false
}
func (s *State) setConnectionIssue(id, source string, err error) {
	h := s.connection(id)
	if err == nil {
		delete(h.issues, source)
	} else if !errors.Is(err, context.Canceled) {
		h.issues[source] = err.Error()
	}
}
func (s *State) connectionProblem(id string) bool {
	if id == "" && s.api.CanHost() {
		network := object(s.host["network"])
		if state := str(network, "state"); state != "" && state != "connected" {
			return true
		}
	}
	h := s.connection(id)
	if len(h.issues) > 0 || h.reconnecting || id != "" && s.connection("").issues["presence"] != "" {
		return true
	}
	for _, d := range s.devices {
		if str(d, "id") == id {
			return !flag(d, "connected")
		}
	}
	return false
}
func (s *State) connectionTitle(id string) string {
	h := s.connection(id)
	if h.reconnecting {
		return "正在重新连接"
	}
	if s.connectionProblem(id) {
		return "连接异常，点击查看详情"
	}
	return "连接正常"
}
func (s *State) connectionIndicator(cx *core.Scope, target func() string) core.Node {
	status := core.NewMemo(cx, func() [2]string { s.read(); id := target(); return [2]string{id, s.connectionTitle(id)} })
	return core.Dynamic(func(cx *core.Scope) core.Node {
		value := status.Get()
		id := value[0]
		icon := lucide.Wifi
		if s.connectionProblem(id) {
			icon = lucide.WifiOff
		}
		node := button(cx, "connection-status-"+id, "", icon, "ghost", func() {
			cx.DismissKeyboard()
			s.connectionDetailTarget = id
			s.dialog.Set("connection-status")
		}).(core.View)
		if s.connection(id).reconnecting {
			node.Children = []core.Node{reconnectingGlyph(cx)}
		}
		node.Label = value[1]
		node.Style.Padding = core.All(8)
		baseStyle := node.StyleFunc
		node.StyleFunc = func() core.Style {
			st := baseStyle()
			if s.connectionProblem(id) && !s.connection(id).reconnecting {
				st.Color = core.RGB(0xd97706)
			}
			return st
		}
		return node
	})
}

// Connection waves travel outwards; the icon never rotates like Refresh.
var connectionWaves = []*core.SVG{
	core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M8.5 16.429a5 5 0 0 1 7 0"/></svg>`)),
	core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 12.859a10 10 0 0 1 14 0"/></svg>`)),
	core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 8.82a15 15 0 0 1 20 0"/></svg>`)),
}

func reconnectingGlyph(cx *core.Scope) core.Node {
	layers := []core.Node{core.Icon{SVG: core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 20h.01"/></svg>`)), Style: core.Style{Position: core.Absolute, Width: core.Pt(18), Height: core.Pt(18)}}}
	for i, svg := range connectionWaves {
		opacity := core.NewAnimatedValue(cx, 1)
		if !ui.ReduceMotion(cx) {
			cx.StartAnimation(core.ValueKeyframes(opacity, core.KeyframeTiming{Duration: 1400 * time.Millisecond, Delay: time.Duration(i) * 180 * time.Millisecond, Iterations: core.Forever}, core.StopAt(0, .25), core.StopAt(.3, 1), core.StopAt(.65, .25), core.StopAt(1, .25)), nil)
		}
		layers = append(layers, core.Icon{SVG: svg, Style: core.Style{Position: core.Absolute, Width: core.Pt(18), Height: core.Pt(18)}, StyleFunc: func() core.Style {
			return core.Style{Color: palette(cx).foreground, Animated: &core.AnimatedStyle{Opacity: opacity}}
		}})
	}
	return core.View{TestID: "connection-retrying", Style: core.Style{Width: core.Pt(18), Height: core.Pt(18)}, Children: layers}
}
func (s *State) connectionDetails(id string) string {
	h := s.connection(id)
	// Multiple automatic reads may fail with the same transport error.
	// Display that error once in the details panel.
	unique := map[string]bool{}
	if id == "" && s.api.CanHost() {
		network := object(s.host["network"])
		if str(network, "state") != "connected" && str(network, "detail") != "" {
			unique[str(network, "detail")] = true
		}
	}
	for _, value := range h.issues {
		unique[value] = true
	}
	if id != "" {
		if value := s.connection("").issues["presence"]; value != "" {
			unique[value] = true
		}
	}
	lines := []string{}
	for value := range unique {
		lines = append(lines, value)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n\n")
}

// A verified handshake starts a fresh state epoch. Late local-state/catalogue
// results from the previous transport must not overwrite the successful result.
func (s *State) acceptConnection(id string, devices []Object) {
	s.presenceGeneration++
	s.presencePolling = false
	if s.pollCancel != nil {
		s.pollCancel()
		s.pollCancel = nil
	}
	s.pollGeneration++
	s.polling = false
	s.devices = devices
	h := s.connection(id)
	h.established = true
	h.issues = map[string]string{}
	delete(s.connection("").issues, "presence")
}

func (s *State) reconnectDevice(id string) { s.reconnect(id, false) }

func (s *State) reconnect(id string, automatic bool) {
	h := s.connection(id)
	if h.reconnecting || s.ctx.Err() != nil {
		return
	}
	h.reconnecting = true
	if !automatic {
		h.established = false
	}
	ctx, cancel := context.WithCancel(s.ctx)
	params := Object{}
	for _, device := range s.devices {
		if str(device, "id") == id {
			params = Object{"code": str(device, "connectionId"), "service": str(device, "service")}
		}
	}
	a := &connectAttempt{params: params, target: id, cancel: cancel, active: true, started: time.Now(), stepStarted: time.Now()}
	a.inherit(h.attempt)
	reason := "手动重新连接"
	detail := "用户发起重新连接"
	if automatic {
		reason = "连接已断开，正在自动重连"
		detail = "后台检测到与设备的连接已断开"
	}
	a.record(connectivity.Progress{ID: "retry", Stage: "retry", Message: reason, Detail: detail, State: "info"})
	a.record(connectivity.Progress{ID: "service", Stage: "service", Message: "正在重新连接服务", State: "running"})
	h.attempt = a
	ctx = connectivity.WithProgress(ctx, func(p connectivity.Progress) {
		s.cx.Post(func() {
			if s.ctx.Err() == nil && h.attempt == a && (a.active || a.complete) {
				a.record(p)
				s.changed()
			}
		})
	})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				s.cx.Post(func() {
					if h.attempt == a && a.active {
						s.changed()
					}
				})
			}
		}
	}()
	s.retrying[id] = time.Now().Add(15 * time.Second)
	s.changed()
	go func() {
		v, err := s.api.Call(ctx, "client.reconnect", Object{"id": id}, "", nil)
		finished := time.Now()
		cancel()
		s.cx.Post(func() {
			if s.ctx.Err() != nil || h.attempt != a {
				return
			}
			a.active = false
			a.finished = finished
			a.complete = err == nil
			if err != nil && !errors.Is(err, context.Canceled) {
				a.err = err.Error()
			}
			h.reconnecting = false
			s.setConnectionIssue(id, "reconnect", err)
			if err == nil {
				s.acceptConnection(id, objects(v))
				if id == s.target {
					s.startSessionWatch()
				}
			}
			s.changed()
			s.poll()
		})
	}()
}

func (s *State) observeLifecycle() {
	cx := s.cx
	observed := s.suspended
	cx.Effect(func() {
		background := cx.Env().Lifecycle() == core.LifecycleBackground
		if background != observed {
			observed = background
			// Do not subscribe the lifecycle effect to page revisions.
			cx.Post(func() { s.setBackground(background) })
		}
	})
}
func (s *State) setBackground(background bool) {
	if background == s.suspended || s.ctx.Err() != nil {
		return
	}
	s.suspended = background
	if background {
		if s.directoryCancel != nil {
			s.directoryCancel()
		}
		if s.pollCancel != nil {
			s.pollCancel()
			s.pollCancel = nil
		}
		s.presenceGeneration++
		s.presencePolling = false
		s.pollGeneration++
		s.polling = false
		if s.watchCancel != nil {
			s.watchCancel()
			s.watchCancel = nil
		}
		return
	}
	// Reuse saved grants and fetch a fresh transcript before resuming deltas.
	// Never replay send/steer or discard a draft when returning to the app.
	for id := range s.retrying {
		delete(s.retrying, id)
	}
	s.refreshTimezone()
	s.directoryChecked = time.Time{}
	// A socket can look open after the OS suspended its network. Probe each
	// previously connected device immediately instead of waiting for a heartbeat.
	for _, d := range s.devices {
		id := str(d, "id")
		if s.connection(id).established && flag(d, "autoReconnect") {
			s.reconnect(id, true)
		}
	}
	s.startSessionWatch()
	s.poll()
}
