package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func ensureSidebarMenu(app *nsgotest.App) {
	if !app.Exists("id:refresh-sessions") {
		activateTestControl(app, "sidebar-menu")
		app.WaitFor("id:refresh-sessions", time.Second)
	}
}
func sidebarMenuAction(app *nsgotest.App, id string) {
	ensureSidebarMenu(app)
	activateTestControl(app, id)
}

func openSectionFilters(app *nsgotest.App, section string) {
	id := sessionFiltersButtonID(section)
	if nsgotest.Target() == "macos" {
		// Native keyboard disclosure; browser tests exercise pointer hover.
		app.Focus("id:section:" + section)
		app.Focus("id:" + id)
		app.Key("Enter")
	} else {
		app.Hover("id:section:" + section)
		app.Hover("id:" + id)
		app.Press("id:" + id)
	}
	app.WaitFor("id:session-filter-panel", time.Second)
}

func openFilterDimension(app *nsgotest.App, dimension string) {
	app.Press("id:filter-menu-" + dimension)
	app.WaitFor("id:filter-"+dimension+"-all", time.Second)
}

func leaveFilterDimension(app *nsgotest.App, dimension string) {
	if app.Exists("id:chat-input") && app.Find("id:chat-input").Focused {
		app.Focus("id:filter-" + dimension + "-all")
	}
	app.Key("Escape")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:filter-" + dimension + "-all") })
}

func closeSectionFilters(app *nsgotest.App) {
	// Escape first returns from a submenu; the second dismisses its menu.
	for range 2 {
		if !app.Exists("id:session-filter-panel") {
			break
		}
		app.Key("Escape")
		app.Settle()
	}
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-filter-panel") })
}

func activateTestControl(app *nsgotest.App, id string) {
	if nsgotest.Target() == "macos" {
		app.Focus("id:" + id)
		app.Key("Space")
	} else {
		app.Press("id:" + id)
	}
}

func TestSectionFiltersStayIndependentAndDiscoverable(t *testing.T) {
	for _, touch := range []bool{false, true} {
		t.Run(choose(touch, "touch", "pointer"), func(t *testing.T) {
			if touch && nsgotest.Target() != "" {
				t.Skip("desktop targets use their native pointer environment; the headless phone fixture covers touch")
			}
			var s *State
			env := core.Environment{Idiom: core.IdiomDesktop, OS: core.OSMacOS}
			if touch {
				env = core.Environment{Idiom: core.IdiomPhone, OS: core.OSAndroid}
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node { s = sidebarInteractionFixture(cx); return s.directory(cx) }, nsgotest.Size(390, 1000), nsgotest.Environment(env))
			for _, section := range []string{"pinned", "all", "recent"} {
				trigger := app.Find("id:" + sessionFiltersButtonID(section))
				if !touch && nsgotest.Target() == "" && trigger.Style.Opacity != 0 {
					t.Fatal("pointer filter is always visible", section)
				}
				if touch && trigger.Style.Opacity == 0 {
					t.Fatal("touch filter requires hover", section)
				}
			}
			openSectionFilters(app, "all")
			app.Find("id:session-grouping")
			openFilterDimension(app, "device")
			app.Press("id:" + sessionFilterID("device", "remote"))
			app.WaitUntil(time.Second, func() bool { return app.Find("id:" + sessionFilterID("device", "remote")).Props.Checked })
			closeSectionFilters(app)
			app.WaitUntil(time.Second, func() bool {
				return !app.Exists("id:"+sessionRowID("all", s.sessions[0])) && app.Exists("id:"+sessionRowID("recent", s.sessions[0])) && app.Exists("id:"+sessionRowID("pinned", s.sessions[2]))
			})
			openSectionFilters(app, "recent")
			if app.Exists("id:session-grouping") {
				t.Fatal("recent exposes grouping")
			}
			openFilterDimension(app, "device")
			app.Press("id:" + sessionFilterID("device", "local"))
			app.WaitUntil(time.Second, func() bool { return app.Find("id:" + sessionFilterID("device", "local")).Props.Checked })
			closeSectionFilters(app)
			app.WaitUntil(time.Second, func() bool {
				return !app.Exists("id:"+sessionRowID("recent", s.sessions[1])) && app.Exists("id:"+sessionRowID("all", s.sessions[1]))
			})
			activateTestControl(app, "section:all")
			openSectionFilters(app, "all")
			if app.Find("id:section:all").Props.Expanded {
				t.Fatal("filter click toggled collapsed section")
			}
		})
	}
}

func TestSidebarFooterAccountAndActions(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		return s.directory(cx)
	}, nsgotest.Size(340, 900))
	if app.Exists("id:theme-toggle") || app.Exists("id:server-settings") || app.Exists("id:account-menu") {
		t.Fatal("secondary controls remain outside the menu")
	}
	footer := globalBounds(app.Find("id:sidebar-footer"))
	if globalBounds(app.Find("id:new-session")).Origin.Y < footer.Origin.Y {
		t.Fatal("new session is not in the footer")
	}
	app.Post(func() {
		s.cx.Post(func() {
			s.accountStatus = Object{"linked": true, "user": Object{"name": "Example account"}}
			s.changed()
		})
	})
	app.WaitFor("text:Example account", time.Second)
	app.Screenshot("sidebar-account-footer")
	ensureSidebarMenu(app)
	app.Find("id:server-settings")
	app.Find("id:account-menu")
	app.Screenshot("sidebar-account-menu")
}
