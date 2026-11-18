package hubui

import (
	"fmt"
	"time"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// A bare Tooltip node has no surface in NSGo. Keep hints on the same bounded,
// themed surface, including icon hints and project paths.
func tooltipCard(cx *core.Scope, children ...core.Node) base.Tooltip {
	return base.Tooltip{
		Delay: 350 * time.Millisecond, Placement: core.PlaceRight, Offset: 8, Margin: 8,
		PopupStyle: core.Style{Padding: core.All(12), Gap: 7, CornerRadius: 10, BorderWidth: 1},
		PopupStyleFunc: func() core.Style {
			c := palette(cx)
			return core.Style{MaxWidth: core.Pt(min(340, max(120, cx.Env().Window().Width-24))), Background: c.elevated, Color: c.foreground, BorderColor: c.border,
				Shadows: []core.BoxShadow{{Offset: core.Point{Y: 4}, Blur: 16, Color: core.RGB(0).Faded(.16)}}}
		}, Children: children,
	}
}

func (s *State) groupInfo(cx *core.Scope, item func() directoryRow) core.Node {
	return core.View{TestID: "group-info-card", Style: core.Style{Gap: 6, MaxWidth: core.Pt(300), MinWidth: core.Pt(0)}, Children: []core.Node{
		dynamicText(cx, func() string { return item().Title }, 13, false),
		dynamicText(cx, func() string { return locales.For(cx).FilterMatches(fmt.Sprint(item().Count)) }, 12, true),
		core.Show{When: func() bool { return item().Path != "" }, Then: func(cx *core.Scope) core.Node {
			text := dynamicText(cx, func() string { return item().Path }, 12, true)
			text.MaxLines = 4
			return text
		}},
	}}
}

func (s *State) sessionTooltip(cx *core.Scope, item func() directoryRow) base.Tooltip {
	line := func(value func() string, size core.Points, muted bool, lines int) core.Node {
		t := dynamicText(cx, value, size, muted)
		t.MaxLines = lines
		return t
	}
	tip := tooltipCard(cx, core.View{TestID: "session-hover-card", Style: core.Style{Gap: 8, MinWidth: core.Pt(0)}, Children: []core.Node{
		line(func() string { return item().Title }, 13, false, 3),
		line(func() string {
			s.read()
			v := item().Session
			return s.environmentName(str(v, "deviceId")) + " · " + s.providerName(str(v, "deviceId"), str(v, "providerId"))
		}, 12, true, 2),
		core.Show{When: func() bool { return str(item().Session, "model") != "" }, Then: func(cx *core.Scope) core.Node {
			return line(func() string { return str(item().Session, "model") }, 12, true, 2)
		}},
		core.Show{When: func() bool { return item().Path != "" }, Then: func(cx *core.Scope) core.Node {
			return line(func() string { return item().Path }, 12, true, 4)
		}},
	}})
	tip.PopupStyle.Width = core.Pt(320)
	return tip
}
