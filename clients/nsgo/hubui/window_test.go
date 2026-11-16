package hubui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"testing"
)

func TestDesktopWindowFrame(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("window action recording uses the simulated platform")
	}
	for _, os := range []core.OS{core.OSMacOS, core.OSWindows, core.OSLinux} {
		t.Run(string(os), func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, newDemoAPI(), false) }, nsgotest.Size(1000, 800), nsgotest.Environment(core.Environment{OS: os, Idiom: core.IdiomDesktop}))
			hidden, controls := false, false
			for _, command := range app.Window {
				if command.Action == core.WindowSetTitleBar && command.TitleBar == core.TitleBarHidden {
					hidden = true
				}
				if command.Action == core.WindowSetControls && command.Controls.Hidden {
					controls = true
				}
			}
			if !hidden || !controls {
				t.Fatal("system title bar or window controls remain visible")
			}
			if app.Find("id:window-drag-region").Props.WindowDrag != core.WindowDragOn {
				t.Fatal("missing drag region")
			}
			for _, id := range []string{"window-minimize", "window-maximize", "window-close"} {
				if app.Find("id:"+id).Props.WindowDrag != core.WindowDragOff {
					t.Fatal("window button intercepts drag incorrectly")
				}
				app.Press("id:" + id)
			}
			want := []core.WindowAction{core.WindowMinimize, core.WindowToggleMaximize, core.WindowClose}
			commands := app.Window[len(app.Window)-3:]
			for i, action := range want {
				if commands[i].Action != action {
					t.Fatalf("window action %d: %v", i, commands[i])
				}
			}
			app.Resize(390, 844)
			toolbar := globalBounds(app.Find("id:window-toolbar"))
			content := globalBounds(app.Find("id:agenthub"))
			if content.Origin.Y != toolbar.Size.Height || toolbar.Size.Width != 390 {
				t.Fatalf("narrow window overlaps content: bar=%v content=%v", toolbar, content)
			}
			app.SetEnvironment(core.Environment{OS: os, Idiom: core.IdiomDesktop, FullScreen: true})
			if app.Exists("id:window-toolbar") {
				t.Fatal("window controls remain in full screen")
			}
			if globalBounds(app.Find("id:agenthub")).Origin.Y != 0 {
				t.Fatal("full screen retains title bar gap")
			}
		})
	}
}
func TestNoDesktopFrameOnMobileOrWeb(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("simulated platform environments")
	}
	for _, os := range []core.OS{core.OSAndroid, core.OS("ios"), core.OS("web")} {
		t.Run(string(os), func(t *testing.T) {
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, newDemoAPI(), false) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: os}))
			if app.Exists("id:window-toolbar") || len(app.Window) > 0 {
				t.Fatal("desktop frame shown on mobile or Web")
			}
		})
	}
}
func TestWindowFrameAppearance(t *testing.T) {
	app := startDemo(t, 1320, 880)
	openDemoChat(t, app)
	app.Screenshot("window-frame-light")
	app.Press("id:theme-toggle")
	app.Screenshot("window-frame-dark")
	if nsgotest.Target() == "web" && app.Exists("id:window-toolbar") {
		t.Fatal("browser must not show desktop controls")
	}
}
