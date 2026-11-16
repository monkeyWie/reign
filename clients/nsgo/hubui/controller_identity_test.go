package hubui

import (
	"context"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestControllerPlatformAndDisplay(t *testing.T) {
	for _, tc := range []struct {
		info             Object
		icon, identifier string
	}{
		{Object{"name": "Levi 的 Mac", "platform": "darwin", "kind": "native", "fingerprint": "0123456789abcdef"}, "apple", "设备标识 · 0123-4567"},
		{Object{"name": "Microsoft Edge 157", "platform": "windows", "kind": "browser", "fingerprint": "0123456789abcdef"}, "edge", ""},
		{Object{"name": "iQOO Z9 Turbo", "platform": "android", "kind": "native"}, "android", ""},
		{Object{"name": "iPad (A16)", "platform": "ios", "kind": "native"}, "apple", ""},
		{Object{"name": "WORKSTATION", "platform": "windows", "kind": "native"}, "windows", ""},
		{Object{"name": "Chrome", "kind": "native", "platform": "windows"}, "windows", ""},
		{Object{"name": "Chrome 157", "platform": "darwin", "kind": "browser"}, "chrome", ""},
		{Object{"name": "Google Chrome 157", "kind": "browser"}, "chrome", ""},
		{Object{"name": "Edge · Android"}, "edge", ""},
		{Object{"name": "Edge · Mac"}, "edge", ""},
		{Object{"name": "Chrome"}, "chrome", ""},
		{Object{"name": "Firefox 157", "kind": "browser", "platform": "android"}, "firefox", ""},
		{Object{"name": "Safari 26", "kind": "browser", "platform": "ios"}, "safari", ""},
		{Object{"name": "Opera 120", "kind": "browser"}, "opera", ""},
		{Object{"name": "Brave 1", "kind": "browser"}, "brave", ""},
		{Object{"name": "Vivaldi 7", "kind": "browser"}, "vivaldi", ""},
		{Object{"name": "Chromium 157", "kind": "browser"}, "chromium", ""},
		{Object{"name": "Unknown browser", "kind": "browser", "platform": "darwin"}, "browser", ""},
		{Object{"name": "ChromeBook"}, "device", ""},
		{Object{"name": "unknown Android name"}, "device", ""},
	} {
		if got := controllerIconKey(tc.info); got != tc.icon {
			t.Errorf("icon %q want %q", got, tc.icon)
		}
		if got := controllerIdentifier(tc.info); got != tc.identifier {
			t.Errorf("identifier %q want %q", got, tc.identifier)
		}
	}
	for _, tc := range []struct{ name, model, want string }{
		{"iPad", "iPad15,7", "iPad (A16)"},
		{"Levi 的 iPad", "iPad15,7", "Levi 的 iPad"},
		{"iPhone", "iPhone17,3", "iPhone 16"},
		{"iPad", "iPad99,99", "iPad"},
	} {
		if got := appleDeviceName(tc.name, tc.model); got != tc.want {
			t.Errorf("Apple name %q want %q", got, tc.want)
		}
	}
}
func TestControllerIdentityRows(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(choose(dark, "dark", "light"), func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetTheme(core.DefaultTheme())
				profiles := []Object{
					{"name": "Levi 的 MacBook Pro", "platform": "darwin", "kind": "native", "fingerprint": "abcdef0123456789"},
					{"name": "Windows PC", "platform": "windows", "kind": "native"},
					{"name": "Linux PC", "platform": "linux", "kind": "native"},
					{"name": "iPad (A16)", "platform": "ios", "kind": "native"},
					{"name": "iQOO Z9 Turbo", "platform": "android", "kind": "native"},
					{"name": "Microsoft Edge 157", "platform": "windows", "kind": "browser"},
					{"name": "Chrome"},
					{"name": "Firefox 157", "kind": "browser", "platform": "android"},
					{"name": "Safari 26", "kind": "browser", "platform": "ios"},
					{"name": "Opera 120", "kind": "browser"},
					{"name": "Brave 1", "kind": "browser"},
					{"name": "Vivaldi 7", "kind": "browser"},
				}
				rows := []core.Node{}
				for _, profile := range profiles {
					rows = append(rows, controllerIdentity(cx, func() Object { return profile }))
				}
				return core.View{Style: core.Style{Padding: core.All(24), Gap: 16}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: rows}
			}, nsgotest.Size(390, 620), nsgotest.Environment(core.Environment{Dark: dark}))
			app.WaitFor("text:Microsoft Edge 157", time.Second)
			app.Find("text:设备标识 · abcd-ef01")
			app.Find("text:iPad (A16)")
			app.Find("text:iQOO Z9 Turbo")
			for _, key := range []string{"windows", "linux", "android", "edge", "chrome", "firefox", "safari", "opera", "brave", "vivaldi"} {
				app.Find("id:controller-platform-" + key)
			}
			app.Screenshot("controller-identities-" + choose(dark, "dark", "light"))
		})
	}
}

type profileCaptureAPI struct {
	*demoAPI
	profile device.ClientInfo
}

func (a *profileCaptureAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	a.profile = device.ClientInfoFrom(ctx)
	return nil, nil
}
func TestProfileSnapshotReachesAPI(t *testing.T) {
	base := &profileCaptureAPI{demoAPI: newDemoAPI()}
	api := &identifiedAPI{API: base}
	info := device.ClientInfo{Name: "vivo V2618A", Platform: "android", Kind: "native"}
	api.setInfo(info)
	_, _ = api.Call(context.Background(), "client.reconnect", nil, "", nil)
	if base.profile != info {
		t.Fatalf("missing profile: %#v", base.profile)
	}
}
