package hubui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func sidebarInteractionFixture(cx *core.Scope) *State {
	s := newState(cx, newDemoAPI())
	s.loadAppearance()
	s.setAppearance("light")
	s.devices = []Object{{"id": "local", "local": true, "connected": true}, {"id": "remote", "name": "Linux Studio", "connected": true}}
	s.workspaces = map[string]workspaceCache{"local": {providers: demoProviders()}, "remote": {providers: demoProviders()}}
	s.sessions = []Object{
		{"id": "build", "deviceId": "local", "providerId": "codex-app", "title": "重构会话目录与设备连接", "model": "gpt-6.1-sol", "cwd": "/work/" + strings.Repeat("nested-project/", 8) + "reign", "projectKey": "shared", "createdAt": 300, "recencyAt": 800000, "updatedAt": 800, "status": "running"},
		{"id": "review", "deviceId": "remote", "providerId": "claude-code", "title": "审查多设备同步与断线恢复", "cwd": "/srv/reign", "projectKey": "shared", "createdAt": 200, "recencyAt": 900000, "updatedAt": 900, "status": "waiting"},
		{"id": "design", "deviceId": "local", "providerId": "codex-cli", "title": "完成侧边栏交互设计", "cwd": "/work/design", "createdAt": 100, "status": "idle"},
	}
	s.pinned[sessionIdentity(s.sessions[2])] = 1
	return s
}

func TestSessionHoverContextMenuAndFilters(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		return core.View{Style: fill, Children: []core.Node{core.View{Style: core.Style{Height: core.Pct(100)}, StyleFunc: func() core.Style { return core.Style{Width: core.Pt(min(390, cx.Env().Window().Width))} }, Children: []core.Node{s.directory(cx)}}}}
	}, nsgotest.Size(800, 1000), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop, OS: core.OSMacOS}))
	local := "id:" + sessionRowID("all", s.sessions[0])
	remote := "id:" + sessionRowID("all", s.sessions[1])
	if nsgotest.Target() != "macos" {
		app.Hover(local)
		app.Advance(450 * time.Millisecond)
		app.WaitFor("id:session-hover-card", time.Second)
	}
	app.RightClick(local)
	app.WaitFor("id:session-pin-action", time.Second)
	if app.Exists("id:session-hover-card") {
		t.Fatal("hover overlaps context menu")
	}
	app.Screenshot("sidebar-context-menu")
	app.Press("id:session-pin-action")
	app.WaitFor("id:"+sessionRowID("pinned", s.sessions[0]), time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-pin-action") })
	if s.selected != nil {
		t.Fatal("context action opened a conversation")
	}
	app.RightClick("id:" + sessionRowID("pinned", s.sessions[0]))
	app.WaitFor("role:menuitem 取消置顶", time.Second)
	app.Press("id:session-pin-action")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-pin-action") })
	if s.pinned[sessionIdentity(s.sessions[0])] != 0 {
		t.Fatal("unpin did not remove the pin")
	}
	if app.Exists("id:session-grouping") {
		t.Fatal("grouping is still permanently visible")
	}
	openSectionFilters(app, "all")
	app.WaitFor("id:session-filter-panel", time.Second)
	app.Screenshot("sidebar-filters-light")
	app.Press("id:" + sessionFilterID("device", "remote"))
	if app.Exists(local) || !app.Exists(remote) {
		t.Fatal("device filter did not update sections")
	}
	app.Press("id:" + sessionFilterID("agent", "codex-app"))
	app.WaitFor("id:all-filter-empty", time.Second)
	if app.Exists(remote) {
		t.Fatal("filters did not intersect")
	}
	app.Press("id:reset-session-filters")
	if !app.Exists(local) || !app.Exists(remote) {
		t.Fatal("reset did not restore sessions")
	}
	app.Scroll("id:session-filter-scroll", 500)
	app.Press("id:" + sessionFilterID("project", sessionProjectKey(s.sessions[0])))
	if len(s.filteredSessions()) != 2 {
		t.Fatal("shared repository filter did not span devices")
	}
	app.Press("id:close-session-filters")
	app.Press("id:clear-all-filters")
	sidebarMenuAction(app, "theme-toggle")

	for _, size := range []core.Size{{Width: 320, Height: 640}, {Width: 390, Height: 844}} {
		app.Resize(size.Width, size.Height)
		openSectionFilters(app, "all")
		app.WaitFor("id:session-filter-panel", time.Second)
		bounds := globalBounds(app.Find("id:session-filter-panel"))
		if bounds.Origin.X < 0 || bounds.Origin.X+bounds.Size.Width > size.Width+.5 || bounds.Origin.Y < 0 || bounds.Origin.Y+bounds.Size.Height > size.Height+.5 {
			t.Fatal("filter panel overflows", size, bounds)
		}
		app.Screenshot(fmt.Sprintf("sidebar-filters-%d-dark", int(size.Width)))
		app.Press("id:close-session-filters")
	}
}

func TestSessionContextMenuLongPress(t *testing.T) {
	if nsgotest.Target() == "web" || nsgotest.Target() == "macos" {
		t.Skip("desktop automation sends a held mouse press, not a finger; touch is covered by the headless phone fixture")
	}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { s = sidebarInteractionFixture(cx); return s.directory(cx) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomPhone, OS: core.OSAndroid}))
	local := "id:" + sessionRowID("all", s.sessions[0])
	app.LongPress(local)
	app.WaitFor("id:session-pin-action", time.Second)
	app.Press("id:session-pin-action")
	if s.pinned[sessionIdentity(s.sessions[0])] == 0 || s.selected != nil {
		t.Fatal("long press failed to pin without opening conversation")
	}
}

func TestSessionFilterIdentityAndMembership(t *testing.T) {
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := sidebarInteractionFixture(cx)
		// Identical leaf names and paths are separate without a shared host key.
		a, b := copyObject(s.sessions[0]), copyObject(s.sessions[1])
		delete(a, "projectKey")
		delete(b, "projectKey")
		a["cwd"], b["cwd"] = "/work/reign", "/work/reign"
		s.sessions = append(s.sessions, a, b)
		a["id"], b["id"] = "local-project", "remote-project"
		s.filters.Projects = map[string]bool{sessionProjectKey(a): true}
		if got := s.filteredSessions(); len(got) != 1 || str(got[0], "id") != "local-project" {
			t.Fatal("project name/path collision", got)
		}
		s.filters.Projects[sessionProjectKey(b)] = true
		if len(s.filteredSessions()) != 2 {
			t.Fatal("multi-select is not a union")
		}
		s.filters.Devices = map[string]bool{"remote": true}
		if len(s.filteredSessions()) != 1 {
			t.Fatal("dimensions do not intersect")
		}
		s.filters = sessionFilters{Agents: map[string]bool{"codex-cli": true}}
		counts := map[string]int{}
		for _, row := range s.directoryRows() {
			if row.Kind == "section" {
				counts[row.Section] = row.Count
			}
		}
		if counts["all"] != 0 || counts["pinned"] != 1 || counts["recent"] != 4 {
			t.Fatal("project filter affected pins or recent sessions", counts)
		}
		return core.View{}
	})
}
