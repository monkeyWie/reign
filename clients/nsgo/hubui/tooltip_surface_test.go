package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestIconTooltipPrefersTopAndFallsBelowAtViewportEdge(t *testing.T) {
	for _, top := range []core.Points{10, 160} {
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			cx.SetTheme(core.DefaultTheme())
			button := labelledIconButton(cx, "tooltip-action", "Action hint", lucide.Plus, func() {}).(core.View)
			tip := button.Tooltip.(base.Tooltip)
			tip.Open = func() bool { return true }
			button.Tooltip = tip
			button.Style.Position, button.Style.Top, button.Style.Left = core.Absolute, core.Pt(top), core.Pt(160)
			return core.View{Style: fill, Children: []core.Node{button}}
		}, nsgotest.Size(360, 300), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
		app.WaitFor("text:Action hint", time.Second)
		trigger, hint := app.Find("id:tooltip-action").Bounds(), app.Find("text:Action hint").Bounds()
		if top == 10 && hint.Origin.Y < trigger.Origin.Y+trigger.Size.Height {
			t.Fatal("top-edge tooltip failed to flip below", trigger, hint)
		}
		if top == 160 && hint.Origin.Y+hint.Size.Height > trigger.Origin.Y {
			t.Fatal("tooltip ignored available top space", trigger, hint)
		}
		if hint.Origin.X < 0 || hint.Origin.X+hint.Size.Width > 360 {
			t.Fatal("tooltip left viewport", hint)
		}
	}
}

// Exercise the actual tooltip surface independently from the platform test
// harness's hover/focus delivery; native pointer behavior is checked separately.
func TestSessionTooltipSurfaceLightAndDark(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		s.sessions[0]["gitInfo"] = Object{"branch": "feature/sidebar-details"}
		item := directoryRow{Title: str(s.sessions[0], "title"), Path: str(s.sessions[0], "cwd"), Session: s.sessions[0]}
		preview := core.NewSignal(cx, false)
		tip := s.sessionTooltip(cx, func() directoryRow { s.read(); return item })
		tip.Open = preview.Get
		trigger := button(cx, "preview-session-hint", item.Title, nil, "ghost", func() { preview.Set(true) }).(core.View)
		trigger.Tooltip = tip
		return core.View{Style: fill, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(300), Margin: core.Edges{Top: 100}}, Children: []core.Node{trigger}},
			button(cx, "preview-dark", "Dark", nil, "ghost", func() { s.setAppearance("dark") }),
		}}
	}, nsgotest.Size(800, 420), nsgotest.Environment(core.Environment{Locale: "zh-CN", OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	app.Press("id:preview-session-hint")
	app.WaitFor("id:session-hover-card", time.Second)
	app.Find("text:feature/sidebar-details")
	app.Screenshot("session-tooltip-surface-light")
	app.Press("id:preview-dark")
	app.Screenshot("session-tooltip-surface-dark")
	card := app.Find("id:session-hover-card").ID
	app.Post(func() {
		s.cx.Post(func() { s.sessions[0]["gitInfo"] = Object{"commit": "abcdef1234567890"}; s.changed() })
	})
	app.WaitFor("text:分离的 HEAD · abcdef12", time.Second)
	if app.Find("id:session-hover-card").ID != card {
		t.Fatal("branch metadata remounted the open tooltip")
	}
	app.Post(func() { s.cx.Post(func() { delete(s.sessions[0], "gitInfo"); s.changed() }) })
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:session-info-branch") })
}
