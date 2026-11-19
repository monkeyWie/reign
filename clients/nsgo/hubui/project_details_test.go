package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestProjectDetailsCountsDirectoriesAndScope(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		s.sessions[0]["project"] = Object{"name": "ReignHub", "roots": []any{"/work/reign", "/work/docs", "/work/reign"}}
		s.sessions[1]["project"] = Object{"name": "ReignHub", "roots": []string{`D:\code\reign`, `D:\code\docs`}}
		for _, ss := range s.sessions[:2] {
			ss["gitInfo"] = Object{"repository": "github.com/team/reign", "branch": "main"}
		}
		item := func() directoryRow {
			for _, r := range s.directoryRows() {
				if r.Kind == "project" {
					return r
				}
			}
			return directoryRow{}
		}
		preview := core.NewSignal(cx, false)
		tip := tooltipCard(cx, s.groupInfo(cx, item, true))
		tip.Open = preview.Get
		trigger := button(cx, "preview-project", "ReignHub", nil, "ghost", func() { preview.Set(true) }).(core.View)
		trigger.Tooltip = tip
		return core.View{Style: fill, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(240), Margin: core.Edges{Top: 80}}, Children: []core.Node{trigger}},
			button(cx, "preview-dark", "Dark", nil, "ghost", func() { s.setAppearance("dark") }),
		}}
	}, nsgotest.Size(800, 560), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Press("id:preview-project")
	app.WaitFor("id:group-info-card", time.Second)
	app.Find("text:2 个会话 · 2 个活跃会话")
	app.Find("text:github.com/team/reign")
	app.Find(`text:"/work/docs · 本机"`)
	app.Find(`text:D:\code\docs · Linux Studio`)
	if len(app.Query("id:group-info-location")) != 5 {
		t.Fatal("duplicate repository/directory or lost project roots")
	}
	app.Screenshot("project-details-light")
	app.Press("id:preview-dark")
	app.Screenshot("project-details-dark")
	card := app.Find("id:group-info-card").ID
	app.Post(func() { s.cx.Post(func() { s.sessions[1]["status"] = "idle"; s.catalogueVersion++; s.changed() }) })
	app.WaitFor("text:2 个会话 · 1 个活跃会话", time.Second)
	if app.Find("id:group-info-card").ID != card {
		t.Fatal("live counts replaced hover panel")
	}
	app.Post(func() {
		s.cx.Post(func() {
			s.filters = sessionFilters{Devices: map[string]bool{"remote": true}}
			s.catalogueVersion++
			s.changed()
		})
	})
	app.WaitUntil(time.Second, func() bool { return len(app.Query("id:group-info-location")) == 3 })
	if app.Exists(`text:"/work/docs"`) || app.Exists("text:1 个活跃会话") {
		t.Fatal("filtered device leaked hover metadata")
	}
	app.Find(`text:D:\code\docs`)
	app.Post(func() { s.cx.Post(func() { s.filters = sessionFilters{}; s.togglePinned(s.sessions[0]) }) })
	app.WaitUntil(time.Second, func() bool { return len(app.Query("id:group-info-location")) == 3 })
	if app.Exists(`text:"/work/docs"`) {
		t.Fatal("pinned session leaked project details")
	}
	app.Resize(390, 560)
	bounds := globalBounds(app.Find("id:group-info-card"))
	if bounds.Origin.X < 0 || bounds.Origin.X+bounds.Size.Width > 390.5 {
		t.Fatal("narrow project panel overflow", bounds)
	}
	app.Screenshot("project-details-narrow")
}
