package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestResponsiveLayoutUsesWidthAcrossPlatforms(t *testing.T) {
	platforms := []core.OS{core.OSMacOS, core.OSAndroid, core.OS("ios"), core.OS("windows")}
	if nsgotest.Target() != "" {
		platforms = platforms[:1]
	} // Real Web keeps the browser's environment.
	for _, platform := range platforms {
		t.Run(string(platform), func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, newDemoAPI(), true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: platform, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
			openDemoChat(t, app)
			assertLayout := func(floating, rail, devices, sessions bool) {
				t.Helper()
				for id, want := range map[string]bool{"conversation-header-fade": floating, "message-navigation": rail, "device-sidebar": devices, "session-sidebar": sessions} {
					if got := app.Exists("id:" + id); got != want {
						t.Fatalf("%s on %s: visible=%v want=%v", id, platform, got, want)
					}
				}
				if floating {
					if globalBounds(app.Find("id:timeline")).Origin.Y != globalBounds(app.Find("id:conversation-header")).Origin.Y {
						t.Fatal("compact toolbar pushed messages down")
					}
				}
			}
			assertLayout(true, false, false, false)
			app.Type("id:chat-input", "跨断点保留草稿")
			app.Screenshot("responsive-narrow")
			app.Resize(800, 844)
			app.WaitFor("id:chat-input", time.Second)
			assertLayout(true, false, true, false)
			app.Resize(1320, 844)
			app.WaitFor("id:chat-input", time.Second)
			assertLayout(false, true, true, true)
			app.Screenshot("responsive-wide")
			app.Resize(390, 844)
			app.WaitFor("id:chat-input", time.Second)
			assertLayout(true, false, false, false)
			if got := app.Find("id:chat-input").Text; got != "跨断点保留草稿" {
				t.Fatalf("resize lost draft: %q", got)
			}
		})
	}
}
