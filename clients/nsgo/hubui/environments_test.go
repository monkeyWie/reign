package hubui

import (
	"context"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"sync/atomic"
	"testing"
	"time"
)

type environmentFixture struct {
	*demoAPI
	remoteOffline atomic.Bool
	routes        chan string
}

func (f *environmentFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.list" {
		return []Object{{"id": "local", "name": "本机", "local": true, "connected": true}, {"id": "remote", "name": "Linux", "connected": !f.remoteOffline.Load()}, {"id": "slow", "name": "Slow", "connected": true}}, nil
	}
	if method == "client.cached" {
		return nil, nil
	}
	if target == "slow" && method == "client.call" && str(r, "method") == "providers" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	session := Object{"id": "same", "providerId": "codex-app", "deviceId": target, "title": "会话 · " + target, "projectKey": "git:fixture:.", "projectDir": "/" + target + "/repo", "cwd": "/" + target + "/repo", "createdAt": 200, "updatedAt": 400, "status": "idle"}
	if target == "local" {
		session["createdAt"] = 300
	}
	if method == "client.sync" {
		if str(p, "provider") != "codex-app" {
			return []Object{}, nil
		}
		return []Object{session}, nil
	}
	if method == "client.call" {
		switch str(r, "method") {
		case "sessions":
			if str(r, "provider") != "codex-app" {
				return Object{"sessions": []Object{}}, nil
			}
			return Object{"sessions": []Object{session}}, nil
		case "transcript":
			return Object{"session": session, "turns": []Object{{"id": "fixture", "status": "completed", "items": []Object{{"id": "reply", "role": "assistant", "text": "消息来自 " + target}}}}}, nil
		case "create":
			f.routes <- target + "/" + str(r, "provider")
			session["id"] = "created"
			return session, nil
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestAllEnvironmentsLoadWithoutSelectionAndKeepRoutes(t *testing.T) {
	if nsgotest.Target() == "macos" {
		t.Skip("native automation surface reports background lifecycle; startup/recovery is exercised headlessly and in Chrome")
	}
	f := &environmentFixture{demoAPI: newDemoAPI(), routes: make(chan string, 4)}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return RootWithAPI(cx, f, true)
	}, nsgotest.Size(1320, 1000), nsgotest.Environment(core.Environment{Locale: "zh-CN"}))
	local, remote := sessionSelector("local", "codex-app", "same"), sessionSelector("remote", "codex-app", "same")
	app.WaitFor(local, 5*time.Second)
	app.WaitFor(remote, 5*time.Second)
	app.Press(remote)
	app.WaitFor("text:消息来自 remote", time.Second)
	app.Type("id:chat-input", "remote draft")
	input := app.Find("id:chat-input").ID
	openSectionFilters(app, "all")
	app.Press("id:session-grouping")
	app.Press("id:group-device-agent")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-filter-panel") })
	if app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "remote draft" {
		t.Fatal("grouping remounted conversation")
	}
	openSectionFilters(app, "all")
	app.Press("id:session-grouping")
	app.Press("id:group-project")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-filter-panel") })
	ss := Object{"deviceId": "remote", "providerId": "codex-app", "id": "same"}
	app.RightClick(remote)
	app.Press("id:session-pin-action")
	remote = "id:" + sessionRowID("pinned", ss)
	app.WaitFor(remote, time.Second)
	app.Press(local)
	app.WaitFor("text:消息来自 local", time.Second)
	app.Type("id:chat-input", "local draft")
	app.Press(remote)
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Text == "remote draft" })
	f.remoteOffline.Store(true)
	app.WaitUntil(4*time.Second, func() bool { return app.Find("id:connection-status-remote").Props.Label == "未连接" })
	if !app.Exists(local) || !app.Exists(remote) || app.Find("id:chat-input").Text != "remote draft" {
		t.Fatal("disconnected environment disrupted directory or draft")
	}
	app.Screenshot("unified-environments-offline")
	f.remoteOffline.Store(false)
	app.WaitUntil(4*time.Second, func() bool { return app.Find("id:connection-status-remote").Props.Label == "连接正常" })
	app.Press("id:new-session")
	app.WaitFor("id:new-session-provider", time.Second)
	app.Press("id:new-session-device")
	app.Press("role:option 本机")
	app.Type("id:new-session-cwd", "/fixture/new")
	app.Press("id:create-session-confirm")
	app.WaitUntil(time.Second, func() bool { return len(f.routes) > 0 })
	if got := <-f.routes; got != "local/codex-app" {
		t.Fatal("new session sent to wrong execution environment", got)
	}
	app.WaitFor("text:消息来自 local", time.Second)
}

type cacheRaceFixture struct {
	*demoAPI
	release chan struct{}
}

func (f *cacheRaceFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.cached" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
		return Object{"providers": demoProviders(), "sessions": []Object{{"id": "deleted", "providerId": "codex-app"}, {"id": "retained", "providerId": "pi"}}}, nil
	}
	if method == "client.call" && str(r, "method") == "providers" {
		return []Object{{"id": "codex-app", "state": "ready", "installed": true}, {"id": "pi", "state": "unavailable", "installed": true}}, nil
	}
	if method == "client.sync" || method == "client.call" && str(r, "method") == "sessions" {
		return []Object{}, nil
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func TestLateCacheKeepsUnavailableAgentWithoutResurrectingDeletedSession(t *testing.T) {
	f := &cacheRaceFixture{newDemoAPI(), make(chan struct{})}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, f)
		s.devices = []Object{{"id": "remote", "connected": true}}
		s.reconcileEnvironments()
		return s.directory(cx)
	}, nsgotest.Size(390, 844))
	app.WaitUntil(time.Second, func() bool { return !s.catalogue("remote").polling })
	close(f.release)
	app.WaitFor(sessionSelector("remote", "pi", "retained"), time.Second)
	if app.Exists(sessionSelector("remote", "codex-app", "deleted")) {
		t.Fatal("late cache resurrected a removed session")
	}
}

func TestTreeExpansionAndCompactSidebar(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		s.loadAppearance()
		s.setAppearance("light")
		s.devices = []Object{{"id": "local", "local": true, "connected": true}, {"id": "remote", "name": "Linux Studio", "connected": true}}
		s.workspaces = map[string]workspaceCache{"local": {providers: demoProviders()}, "remote": {providers: demoProviders()}}
		s.sessions = []Object{
			{"id": "build", "deviceId": "local", "providerId": "codex-app", "title": "重构会话目录与设备连接", "cwd": "/work/reign", "projectKey": "shared", "createdAt": 300, "updatedAt": 800, "status": "running"},
			{"id": "review", "deviceId": "remote", "providerId": "claude-code", "title": "审查多设备同步与断线恢复", "cwd": "/srv/reign", "projectKey": "shared", "createdAt": 200, "updatedAt": 900, "status": "waiting"},
			{"id": "design", "deviceId": "local", "providerId": "pi", "title": "完成侧边栏交互设计", "cwd": "/work/design", "createdAt": 100, "status": "idle"},
		}
		s.pinned[sessionIdentity(s.sessions[2])] = 1
		return s.directory(cx)
	}, nsgotest.Size(390, 1000), nsgotest.Environment(core.Environment{Locale: "zh-CN"}))
	app.Screenshot("unified-sidebar-projects-light")
	openSectionFilters(app, "all")
	app.Press("id:session-grouping")
	app.Press("id:group-device-agent")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-filter-panel") })
	app.Press("id:section:pinned")
	app.Press("id:section:recent")
	app.Screenshot("unified-sidebar-device-agent-light")
	// Actual expansion control must change visible descendants.
	root := s.sessionTree(uniqueSessions(s.sessions))[0]
	app.Press("id:tree:" + root.Key)
	if app.Exists(sessionSelector("local", "codex-app", "build")) {
		t.Fatal("collapsed device kept its descendants visible")
	}
	app.Press("id:tree:" + root.Key)
	app.WaitFor(sessionSelector("local", "codex-app", "build"), time.Second)
	for _, width := range []core.Points{320, 390} {
		app.Resize(width, 1000)
		for _, id := range []string{"session-sidebar", "session-filters", "connect-device", "sidebar-menu"} {
			bounds := globalBounds(app.Find("id:" + id))
			if bounds.Origin.X < 0 || bounds.Origin.X+bounds.Size.Width > width+.5 {
				t.Fatal("sidebar overflows", id, width, bounds)
			}
		}
	}
	sidebarMenuAction(app, "theme-toggle")
	app.Screenshot("unified-sidebar-device-agent-dark")
}
