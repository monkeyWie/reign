package hubui

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"strings"
)

// Mobile pages are real native stack entries. Keeping the device page below
// sessions and chat lets system Back and interactive swipe pop the same routes.
func (s *State) phonePages(cx *core.Scope) core.Node {
	nav := base.NewNav(cx, "devices")
	s.phoneNav = nav
	cx.Cleanup(func() {
		if s.phoneNav == nav {
			s.phoneNav = nil
		}
	})
	if s.target != "" {
		nav.Push("sessions:" + s.target)
	}
	if s.narrowChat && s.selected != nil {
		nav.Push("chat:" + s.key())
	}
	return nav.Stack(fill, func(cx *core.Scope, route string, _ func() int) base.NavPage {
		page := base.NavPage{TestID: "page-" + strings.Split(route, ":")[0], HeaderHidden: true, Style: fill}
		switch {
		case route == "local":
			page.Children = []core.Node{button(cx, "back-local", "设备", lucide.ArrowLeft, "ghost", func() { nav.Pop(1) }), s.local(cx)}
		case route == "devices":
			page.Children = []core.Node{s.phoneDevices(cx)}
		case strings.HasPrefix(route, "sessions:"):
			page.Children = []core.Node{s.directory(cx)}
			page.OnDismiss = func() { s.rememberDraft(); s.narrowChat = false; s.changed() }
		default:
			page.Children = []core.Node{s.conversation(cx, true)}
			page.OnDismiss = func() { s.rememberDraft(); s.narrowChat = false; s.changed() }
		}
		return page
	})
}
func (s *State) showPhoneSessions() {
	if s.phoneNav == nil {
		return
	}
	routes := []string{"devices"}
	if s.target != "" {
		routes = append(routes, "sessions:"+s.target)
	}
	s.narrowChat = false
	s.phoneNav.SetRoutesKeeping(func(a, b string) bool { return a == b }, routes...)
}
func (s *State) showPhoneChat() {
	if s.phoneNav == nil {
		return
	}
	s.phoneNav.SetRoutesKeeping(func(a, b string) bool { return a == b }, "devices", "sessions:"+s.target, "chat:"+s.key())
}
func (s *State) backSessions() {
	s.rememberDraft()
	s.narrowChat = false
	if s.phoneNav != nil {
		s.phoneNav.Pop(1)
	}
	s.changed()
}
func (s *State) backDevices() {
	s.rememberDraft()
	s.narrowChat = false
	if s.phoneNav != nil {
		s.phoneNav.PopToRoot()
	}
	s.changed()
}
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
		return "离线 · 可查看已缓存会话"
	}
	path := str(d, "path")
	if path == "" {
		return "已连接 · 正在协商连接路径"
	}
	return path
}
func (s *State) phoneDevices(cx *core.Scope) core.Node {
	nodes := []core.Node{
		core.View{Style: core.Style{Padding: core.All(20), Gap: 16}, Children: []core.Node{
			row(label(cx, "ReignHub", 24, false), spacer(), core.Show{When: func() bool { s.read(); return s.connectionProblem("") }, Then: func(cx *core.Scope) core.Node { return s.connectionIndicator(cx, func() string { return "" }) }}, button(cx, "mobile-account", "", lucide.User, "ghost", func() { s.dialog.Set("account") }), button(cx, "theme-toggle", "", lucide.SunMoon, "ghost", s.toggleTheme)),
			label(cx, "选择设备，查看与管理它的 Agent 会话", 13, true),
			row(button(cx, "connect-device", "连接设备", lucide.Plus, "primary", func() { s.scanned = nil; s.dialog.Set("connect") }), core.Show{When: func() bool { return isMobile(cx) }, Then: func(cx *core.Scope) core.Node {
				return button(cx, "scan-qr", "扫一扫", lucide.ScanLine, "outline", func() { s.scan(cx) })
			}}),
		}},
	}
	if s.api.CanHost() {
		nodes = append(nodes, button(cx, "home", "这台电脑的 Agent 与服务设置", lucide.Monitor, "outline", func() { s.phoneNav.Push("local") }))
	}
	nodes = append(nodes, core.Scroll{Style: fill, Children: []core.Node{core.View{Style: core.Style{Padding: core.Symmetric(4, 16), Gap: 10}, Children: []core.Node{
		core.Show{When: func() bool { s.read(); return len(s.devices) == 0 }, Then: func(cx *core.Scope) core.Node {
			return card(cx, label(cx, "尚未连接设备", 16, false), label(cx, "输入电脑上的设备 ID，或扫描设备二维码开始连接。已配对设备会保存在本机。", 13, true))
		}},
		core.Each(func() []Object { s.read(); return s.devices }, func(d Object) string { return str(d, "id") }, func(cx *core.Scope, d func() Object, _ func() int) core.Node {
			return core.View{TestID: "device-" + str(d(), "id"), Role: core.RoleButton, LabelFunc: func() string { return deviceName(d()) }, OnPress: func(core.PressEvent) { s.selectDevice(str(d(), "id")) }, Style: core.Style{Padding: core.All(16), Gap: 9, CornerRadius: 10, BorderWidth: 1, MinHeight: core.Pt(96)}, StyleFunc: func() core.Style {
				p := palette(cx)
				return core.Style{Background: p.background, BorderColor: p.border, Hover: &core.Style{Background: p.muted}}
			}, Children: []core.Node{
				row(glyph(cx, lucide.Monitor, 20), singleLine(cx, func() string { return deviceName(d()) }, 16, false), spacer(), s.connectionIndicator(cx, func() string { return str(d(), "id") }), glyph(cx, lucide.ChevronRight, 18)),
				dynamicText(cx, func() string { return "ID " + str(d(), "connectionId") }, 12, true),
				dynamicText(cx, func() string { return deviceStatus(d()) }, 12, true),
			}}
		}),
	}}}})
	return core.View{TestID: "mobile-devices", Style: fill, Children: nodes}
}
