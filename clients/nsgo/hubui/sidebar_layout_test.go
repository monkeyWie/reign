package hubui

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func workspaceLayoutFixture(cx *core.Scope) *State {
	s := sidebarInteractionFixture(cx)
	s.ready, s.suspended = true, true
	s.target, s.selected = "local", s.sessions[2]
	for _, ss := range s.sessions {
		id := str(ss, "deviceId")
		w := s.workspaces[id]
		w.sessions = append(w.sessions, ss)
		s.workspaces[id] = w
	}
	s.narrowChat = true
	s.transcript = Object{"turns": []Object{{"id": "fixture", "status": "completed", "items": []Object{{"id": "answer", "role": "assistant", "text": "侧边栏收起后，当前会话保持不变。"}}}}}
	s.rebuildMessages()
	s.host = Object{"name": "Studio MacBook Pro", "connectionId": "123456", "status": "在线 · 可连接", "grants": []Object{
		{"client": "phone", "name": "测试手机", "platform": "android"},
		{"client": "browser", "name": "Chrome", "kind": "browser"},
	}}
	s.installWorkspaceNavigation()
	installScrollbars(cx)
	return s
}

func TestSidebarTogglePreservesWorkspace(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 820), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Type("id:chat-input", "保留未发送草稿")
	input := app.Find("id:chat-input").ID
	sidebar := app.Find("id:session-list").ID
	before := globalBounds(app.Find("id:conversation"))
	toggle := globalBounds(app.Find("id:toggle-sidebar"))
	directory := globalBounds(app.Find("id:sidebar-container"))
	if math.Abs(float64(toggle.Origin.X+toggle.Size.Width-directory.Origin.X-directory.Size.Width+12)) > 1 {
		t.Fatal("toggle is not aligned to the sidebar's right edge", toggle, directory)
	}
	if before.Origin.Y != directory.Origin.Y || before.Size.Height != directory.Size.Height {
		t.Fatal("sidebar and conversation do not share top/bottom edges", before, directory)
	}
	if app.Find("id:conversation").Style.CornerRadius != app.Find("id:session-sidebar").Style.CornerRadius || app.Find("id:conversation").Style.BorderWidth != 0 {
		t.Fatal("conversation has an extra rounded frame beside the flat sidebar")
	}
	add := globalBounds(app.Find("id:connect-device"))
	if math.Abs(float64(toggle.Origin.X+toggle.Size.Width/2-add.Origin.X-add.Size.Width/2)) > 1 {
		t.Fatal("title-bar and sidebar trailing actions do not share an alignment axis", toggle, add)
	}
	if app.Exists("id:window-toolbar") {
		bar := globalBounds(app.Find("id:window-toolbar"))
		if math.Abs(float64(toggle.Origin.Y+toggle.Size.Height/2-bar.Origin.Y-bar.Size.Height/2)) > 1 || app.Find("id:toggle-sidebar").Props.WindowDrag != core.WindowDragOff {
			t.Fatal("toggle is not centered on the native title bar or starts dragging")
		}
	}
	stripe := app.Find("id:sidebar-titlebar-surface")
	stripeBounds := globalBounds(stripe)
	if stripe.Style.Background != app.Find("id:session-sidebar").Style.Background || stripeBounds.Origin.X != directory.Origin.X || stripeBounds.Size.Width != directory.Size.Width {
		t.Fatal("sidebar does not continue through the title bar", stripeBounds, directory)
	}
	app.Post(func() { s.cx.Post(func() { s.sessionWidth.Set(360) }) })
	app.Settle()
	resizedToggle := globalBounds(app.Find("id:toggle-sidebar"))
	resizedAdd := globalBounds(app.Find("id:connect-device"))
	if math.Abs(float64(resizedToggle.Origin.X+resizedToggle.Size.Width/2-resizedAdd.Origin.X-resizedAdd.Size.Width/2)) > 1 {
		t.Fatal("resizing the sidebar detached its toolbar alignment")
	}
	app.Post(func() { s.cx.Post(func() { s.sessionWidth.Set(300) }) })
	app.Settle()
	app.Screenshot("sidebar-toggle-expanded-light")
	app.Press("id:toggle-sidebar")
	after := globalBounds(app.Find("id:conversation"))
	if after.Size.Width < before.Size.Width+260 || after.Origin.X > 10 {
		t.Fatal("collapsed sidebar still consumes conversation space", before, after)
	}
	if app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "保留未发送草稿" {
		t.Fatal("toggling remounted the editor or lost the draft")
	}
	app.Find("role:button 展开侧边栏")
	app.Screenshot("sidebar-toggle-collapsed-light")
	app.Press("id:toggle-sidebar")
	if app.Find("id:session-list").ID != sidebar || globalBounds(app.Find("id:conversation")) != before {
		t.Fatal("restore remounted the directory or changed its width")
	}
	sidebarMenuAction(app, "theme-toggle")
	app.Screenshot("sidebar-toggle-expanded-dark")
	app.Press("id:toggle-sidebar")
	app.Resize(649, 820)
	app.WaitFor("id:back-sessions", time.Second)
	app.Press("id:back-sessions")
	app.WaitFor("id:session-sidebar", time.Second)
	if app.Exists("id:toggle-sidebar") {
		t.Fatal("compact navigation inherited wide sidebar controls")
	}
	app.Resize(650, 820)
	app.WaitFor("role:button 展开侧边栏", time.Second)
	app.Press("id:toggle-sidebar")
	app.WaitFor("id:session-sidebar", time.Second)
}

func TestLocalManagementFlatSurface(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := workspaceLayoutFixture(cx)
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Press("id:device-local")
	app.WaitFor("id:device-code", time.Second)
	app.Screenshot("local-management-flat-light")
	app.Press("id:dialog-local-close")
	sidebarMenuAction(app, "theme-toggle")
	app.Press("id:device-local")
	app.Screenshot("local-management-flat-dark")
	app.Resize(390, 844)
	app.WaitFor("id:copy-device-id", time.Second)
	for _, id := range []string{"copy-device-id", "show-qr", "configure-service"} {
		bounds := globalBounds(app.Find("id:" + id))
		if bounds.Origin.X < 0 || bounds.Origin.X+bounds.Size.Width > 390 {
			t.Fatal("local management action overflows the narrow dialog", id, bounds)
		}
	}
	app.Screenshot("local-management-flat-390-dark")
}
