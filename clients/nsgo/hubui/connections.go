package hubui

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

// Connection health belongs to a device, not to the last user operation. Keep
// independent read failures until that read succeeds; a heartbeat must not
// hide a failed catalogue or transcript refresh.
type connectionHealth struct {
	issues                       map[string]string
	reconnecting, needsReconnect bool
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
	h.needsReconnect = false
	for _, value := range h.issues {
		if transportError(errors.New(value)) {
			h.needsReconnect = true
		}
	}
}
func (s *State) connectionProblem(id string) bool {
	h := s.connection(id)
	if len(h.issues) > 0 || h.reconnecting || id != "" && len(s.connection("").issues) > 0 {
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
		if s.connection(id).reconnecting {
			icon = lucide.RefreshCw
		}
		node := button(cx, "connection-status-"+id, "", icon, "ghost", func() {
			cx.DismissKeyboard()
			s.connectionDetailTarget = id
			s.dialog.Set("connection-status")
		}).(core.View)
		node.Label = value[1]
		node.Style.Padding = core.All(8)
		baseStyle := node.StyleFunc
		node.StyleFunc = func() core.Style {
			st := baseStyle()
			if s.connectionProblem(id) {
				st.Color = core.RGB(0xd97706)
			}
			return st
		}
		return node
	})
}
func (s *State) connectionDetails(id string) string {
	h := s.connection(id)
	// Multiple automatic reads may fail with the same transport error.
	// Display that error once in the details panel.
	unique := map[string]bool{}
	for _, value := range h.issues {
		unique[value] = true
	}
	if id != "" {
		for _, value := range s.connection("").issues {
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
func (s *State) reconnectDevice(id string) {
	h := s.connection(id)
	if h.reconnecting || s.ctx.Err() != nil {
		return
	}
	h.reconnecting = true
	s.retrying[id] = time.Now().Add(15 * time.Second)
	s.changed()
	go func() {
		v, err := s.api.Call(s.ctx, "client.reconnect", Object{"id": id}, "", nil)
		s.cx.Post(func() {
			if s.ctx.Err() != nil {
				return
			}
			h.reconnecting = false
			s.setConnectionIssue(id, "reconnect", err)
			if err == nil {
				h.needsReconnect = false
				s.devices = objects(v)
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
		background := isMobile(cx) && cx.Env().Lifecycle() == core.LifecycleBackground
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
		if s.pollCancel != nil {
			s.pollCancel()
			s.pollCancel = nil
		}
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
	s.startSessionWatch()
	s.autoReconnect()
	s.poll()
}
