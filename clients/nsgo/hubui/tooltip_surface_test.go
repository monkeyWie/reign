package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// Exercise the actual tooltip surface independently from the platform test
// harness's hover/focus delivery; native pointer behavior is checked separately.
func TestSessionTooltipSurfaceLightAndDark(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		item := directoryRow{Title: str(s.sessions[0], "title"), Path: str(s.sessions[0], "cwd"), Session: s.sessions[0]}
		preview := core.NewSignal(cx, false)
		tip := s.sessionTooltip(cx, func() directoryRow { return item })
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
	app.Screenshot("session-tooltip-surface-light")
	app.Press("id:preview-dark")
	app.Screenshot("session-tooltip-surface-dark")
}
