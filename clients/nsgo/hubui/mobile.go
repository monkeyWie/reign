package hubui

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"strings"
)

// The compact workspace has one aggregate list and one conversation page.
func (s *State) phonePages(cx *core.Scope) core.Node {
	nav := base.NewNav(cx, "sessions")
	s.phoneNav = nav
	cx.Effect(func() {
		routes := nav.Routes()
		s.phoneRoute.Set(routes[len(routes)-1])
		if nav.Depth() > 0 {
			s.clearRootBack()
		}
	})
	cx.Cleanup(func() {
		if s.phoneNav == nav {
			s.phoneNav = nil
			s.phoneRoute.Set("")
		}
	})
	if s.narrowChat && s.selected != nil {
		nav.Push("chat:" + s.key())
	}
	return nav.Stack(fill, func(cx *core.Scope, route string, _ func() int) base.NavPage {
		page := base.NavPage{TestID: "page-" + strings.Split(route, ":")[0], HeaderHidden: true, Style: fill}
		if route == "sessions" {
			page.Children = []core.Node{s.directory(cx)}
		} else {
			page.Children = []core.Node{s.conversation(cx, true)}
			page.OnDismiss = func() { s.rememberDraft(); s.narrowChat = false; s.changed() }
		}
		return page
	})
}
func (s *State) showPhoneSessions() {
	s.narrowChat = false
	if s.phoneNav != nil {
		s.phoneNav.SetRoutesKeeping(func(a, b string) bool { return a == b }, "sessions")
	}
}
func (s *State) showPhoneChat() {
	if s.phoneNav != nil {
		s.phoneNav.SetRoutesKeeping(func(a, b string) bool { return a == b }, "sessions", "chat:"+s.key())
	}
}
func (s *State) backSessions() {
	s.rememberDraft()
	s.narrowChat = false
	if s.phoneNav != nil {
		s.phoneNav.PopToRoot()
	} else {
		s.rememberSession()
		if s.watchCancel != nil {
			s.watchCancel()
		}
		s.sessionGeneration++
		s.selected, s.transcript = nil, nil
		s.messageRows, s.approvals, s.uploads = nil, nil, nil
		s.transcriptSyncing, s.transcriptLoading = false, false
		s.draft.Set("")
	}
	s.changed()
}
func (s *State) backDevices() { s.backSessions() }
func deviceName(d Object) string {
	if n := strings.TrimSpace(str(d, "name")); n != "" && n != "RAS" {
		return n
	}
	if code := str(d, "connectionId"); code != "" {
		return "设备 " + code
	}
	return "未命名设备"
}
func deviceStatus(d Object) string {
	if !flag(d, "connected") {
		if online, known := d["online"].(bool); known {
			if online {
				return "在线 · 点击连接"
			}
			return "离线"
		}
		return "在线状态暂不可用"
	}
	path := str(d, "path")
	if path == "" {
		return "已连接 · 正在协商连接路径"
	}
	return path
}
