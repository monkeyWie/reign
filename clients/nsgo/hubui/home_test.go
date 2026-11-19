package hubui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type homeDashboardAPI struct {
	*localDemo
	enable  chan error
	enabled atomic.Bool
	calls   atomic.Int32
}

func (a *homeDashboardAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	switch method {
	case "host.info":
		v, err := a.localDemo.Call(ctx, method, p, target, r)
		host := object(v)
		host["grants"] = []Object{{"client": "fixture-phone", "name": "Authorized phone", "platform": "android"}}
		return host, err
	case "providers":
		return []any{
			Object{"id": "codex-app", "name": "Codex App", "state": "connected", "installed": true, "detail": "已通过本地 IPC 接入"},
			Object{"id": "fixture-acp", "name": "Fixture Agent", "state": choose(a.enabled.Load(), "ready", "disabled"), "enabled": a.enabled.Load(), "installed": true, "detail": "隔离测试 Agent"},
			Object{"id": "fixture-missing", "name": "Optional Agent", "state": "unavailable", "installed": false},
		}, nil
	case "host.agent.enabled":
		a.calls.Add(1)
		select {
		case err := <-a.enable:
			if err == nil {
				a.enabled.Store(flag(p, "enabled"))
			}
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return a.localDemo.Call(ctx, method, p, target, r)
}

func TestHomeOverviewKeepsSidebarAndAgentFeedback(t *testing.T) {
	api := &homeDashboardAPI{localDemo: &localDemo{newDemoAPI()}, enable: make(chan error, 1)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, api)
		// This fixture tests controls, independently of native window focus.
		// Background suspension has separate lifecycle regressions.
		s.keepAlive = true
		s.applyColors()
		s.installWorkspaceNavigation()
		s.initialize()
		return s.workspaceView(cx)
	}, nsgotest.Size(1180, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.WaitFor("id:home-agent-switch-fixture-acp", 3*time.Second)
	app.Find("id:session-sidebar")
	app.Find("text:266 556")
	app.Find("text:去安装")
	if !app.Find("id:home-agent-switch-codex-app").Props.Checked || !app.Find("id:home-agent-switch-fixture-missing").Props.Disabled {
		t.Fatal("default on / unavailable switch state")
	}
	if app.Exists("text:已通过本地 IPC 接入") {
		t.Fatal("home exposed implementation detail")
	}
	if app.Exists("id:revoke-fixture-phone") {
		t.Fatal("home exposed controller details or an unusable activation")
	}
	sidebar := app.Find("id:session-list").ID
	home := app.Find("id:workspace-home").ID
	app.Screenshot("home-device-agents-light")
	sidebarMenuAction(app, "theme-toggle")
	app.Screenshot("home-device-agents-dark")
	app.Press("id:home-copy-device-id")
	if app.Clipboard != "266556" {
		t.Fatal("home did not copy connection ID")
	}
	app.Press("id:home-show-qr")
	app.Find("id:dialog-qr")
	app.Press("id:dialog-done")
	app.Press("id:home-manage-access")
	app.Find("id:revoke-fixture-phone")
	app.Press("id:dialog-local-access-close")
	app.Press("id:home-agent-switch-fixture-acp")
	app.WaitFor("id:home-agent-switch-fixture-acp-loading", time.Second)
	app.PressAt("id:home-agent-switch-fixture-acp", core.Point{X: 8, Y: 8})
	app.WaitUntil(time.Second, func() bool { return api.calls.Load() == 1 })
	api.enable <- errors.New("Fixture activation unavailable")
	app.WaitFor("text:Fixture activation unavailable", time.Second)
	if app.Find("id:home-agent-switch-fixture-acp").Props.Checked {
		t.Fatal("failed preference stayed enabled")
	}
	app.Press("id:home-agent-switch-fixture-acp")
	app.WaitFor("id:home-agent-switch-fixture-acp-loading", time.Second)
	api.enable <- nil
	app.WaitUntil(3*time.Second, func() bool {
		return !app.Exists("id:home-agent-switch-fixture-acp-loading") && app.Find("id:home-agent-switch-fixture-acp").Props.Checked && !app.Find("id:home-agent-switch-fixture-acp").Props.Disabled
	})
	if app.Find("id:session-list").ID != sidebar || app.Find("id:workspace-home").ID != home {
		t.Fatal("home update remounted the workspace")
	}
	openDemoChat(t, app)
	app.Type("id:chat-input", "Home keeps this draft")
	app.Press("id:brand-home")
	app.Find("id:workspace-home")
	if app.Find("id:session-list").ID != sidebar {
		t.Fatal("returning home remounted the sidebar")
	}
	openDemoChat(t, app)
	if app.Find("id:chat-input").Text != "Home keeps this draft" {
		t.Fatal("returning home lost the draft")
	}
}

func TestHomeOverviewAtNarrowDesktopWidth(t *testing.T) {
	app := startDemo(t, 650, 844)
	app.WaitFor("id:home-agent-codex-app", 3*time.Second)
	for _, id := range []string{"home-copy-device-id", "home-show-qr", "home-manage-access", "home-agent-codex-app"} {
		b := globalBounds(app.Find("id:" + id))
		if b.Origin.X < 300 || b.Origin.X+b.Size.Width > 650 {
			t.Fatalf("%s overflows its content pane: %v", id, b)
		}
	}
	app.Screenshot("home-device-agents-narrow")
}

func TestControllerHomeOffersRemoteConnection(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return RootWithAPI(cx, &mobileDemo{newDemoAPI()}, false)
	}, nsgotest.Size(1000, 800))
	app.Find("id:session-sidebar")
	if app.Exists("id:home-device-code") {
		t.Fatal("controller-only app presented a local host")
	}
	app.Press("id:welcome-connect")
	app.Find("id:dialog-connect")
}
