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
		return []Object{
			{"id": "codex-app", "name": "Codex App", "installed": true, "state": "unsupported", "detail": "Windows 已支持 Codex App 检测，暂不支持接入远控", "capabilities": Object{"desktop": true}},
			{"id": "codex-cli", "name": "Codex CLI", "installed": true, "state": "unavailable", "detail": "后台未运行"},
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
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, api, true) }, nsgotest.Size(width, height))
	app.WaitFor("id:server-settings", 3*time.Second)
	app.Press("id:server-settings")
	app.WaitFor("id:settings-nav-agents", 3*time.Second)
	app.Press("id:settings-nav-agents")
	app.WaitFor("text:已安装 · Windows 已支持 Codex App 检测，暂不支持接入远控", 3*time.Second)
	status := app.Find("text:已安装 · Windows 已支持 Codex App 检测，暂不支持接入远控")
	if status.Frame.Size.Width > width {
		t.Fatalf("provider status overflows viewport: %v > %v", status.Frame.Size.Width, width)
	}
	if app.Exists("id:enable-codex-app") {
		t.Fatal("unsupported App offered activation")
	}
	app.Find("id:enable-codex-cli")
	app.Screenshot("unsupported-codex-app-" + name)
}
