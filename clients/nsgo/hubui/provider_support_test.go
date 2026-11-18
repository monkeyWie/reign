package hubui

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type unsupportedAppAPI struct{ *demoAPI }

func (a *unsupportedAppAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "providers" {
		return []any{
			Object{"id": "codex-app", "name": "Codex App", "installed": true, "state": "unsupported", "detail": "Windows 已支持 Codex App 检测，暂不支持接入远控", "capabilities": Object{"desktop": true}},
			Object{"id": "codex-cli", "name": "Codex CLI", "installed": true, "state": "unavailable", "detail": "后台未运行"},
		}, nil
	}
	return a.demoAPI.Call(ctx, method, p, target, r)
}

func TestInstalledUnsupportedAppDoesNotOfferEnable(t *testing.T) {
	checkUnsupportedApp(t, 1320, 880, "desktop")
}

func TestInstalledUnsupportedAppDoesNotOfferEnableNarrow(t *testing.T) {
	checkUnsupportedApp(t, 390, 844, "narrow")
}

func checkUnsupportedApp(t *testing.T, width, height core.Points, name string) {
	t.Helper()
	api := &unsupportedAppAPI{newDemoAPI()}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := newState(cx, api)
		s.keepAlive = true // Control layout is independent of native window focus.
		s.loadAppearance()
		s.installWorkspaceNavigation()
		s.initialize()
		return s.workspaceView(cx)
	}, nsgotest.Size(width, height), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "en"}))
	app.WaitFor("id:server-settings", 3*time.Second)
	app.Press("id:server-settings")
	app.WaitFor("id:settings-nav-agents", 3*time.Second)
	app.Press("id:settings-nav-agents")
	app.WaitFor("id:settings-agent-switch-codex-app", 3*time.Second)
	status := app.Find("id:settings-agent-switch-codex-app")
	if status.Frame.Size.Width > width {
		t.Fatalf("provider status overflows viewport: %v > %v", status.Frame.Size.Width, width)
	}
	if !app.Find("id:settings-agent-switch-codex-app").Props.Disabled {
		t.Fatal("unsupported App offered activation")
	}
	app.Find("id:settings-configure-codex-cli")
	app.Screenshot("unsupported-codex-app-" + name)
}
