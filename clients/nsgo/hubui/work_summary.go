package hubui

import (
	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

// Completed process records stay available on demand, separate from the final
// answer. Mount the bounded, virtualized detail list only when expanded.
func (s *State) workSummary(cx *core.Scope, get func() Message) core.Node {
	id := core.Untracked(cx, get).ID
	open := core.NewSignal(cx, false)
	title := func() string { return workSummaryTitle(locales.For(cx), get().DurationMS) }
	toggle := button(cx, "work-toggle-"+id, "", nil, "ghost", func() { open.Set(!open.Get()) }).(core.View)
	toggle.LabelFunc = title
	toggle.Children = []core.Node{core.Text{ContentFunc: title, MaxLines: 1, Style: core.Style{FontSize: 13, LineHeight: 18, FlexShrink: 1, MinWidth: core.Pt(0)}}}
	toggle.Expanded = open.Get
	toggle.Style.Padding, toggle.Style.AlignSelf = core.Symmetric(8, 0), core.AlignStart
	toggle.StyleFunc = func() core.Style {
		c := palette(cx)
		return core.Style{Color: c.mutedText, Hover: &core.Style{Color: c.foreground}, Active: &core.Style{Opacity: core.Alpha(.65)}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: c.focus}}
	}
	toggle.Children = append(toggle.Children, core.Icon{SVGFunc: func() *core.SVG {
		if open.Get() {
			return lucide.ChevronDown()
		}
		return lucide.ChevronRight()
	}, Style: core.Style{Width: core.Pt(13), Height: core.Pt(13)}})
	return core.View{TestID: "message-" + id, Style: core.Style{Padding: core.Edges{Top: 8, Bottom: 4}, MinWidth: core.Pt(0)}, Children: []core.Node{toggle,
		core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
			return core.List{TestID: "work-details-" + id, Count: func() int { return len(get().Details) },
				Key: func(i int) any { return get().Details[i].ID }, EstimatedSize: 84,
				StyleFunc: func() core.Style {
					return core.Style{Height: core.Pt(min(420, core.Points(len(get().Details)*84))), MinWidth: core.Pt(0)}
				},
				Row: func(cx *core.Scope, row core.ListRow) core.Node {
					key := core.NewMemo(cx, func() string {
						v := get().Details
						if row.Index() >= 0 && row.Index() < len(v) {
							return v[row.Index()].ID
						}
						return ""
					})
					return core.Dynamic(func(cx *core.Scope) core.Node {
						if key.Get() == "" {
							return nil
						}
						return s.message(cx, func() Message {
							v := get().Details
							if row.Index() >= 0 && row.Index() < len(v) {
								return v[row.Index()]
							}
							return Message{}
						})
					})
				}}
		}},
	}}
}

func workSummaryTitle(msgs locales.Messages, duration *int64) string {
	if duration == nil || *duration < 0 {
		return msgs.WorkDetails()
	}
	seconds := *duration / 1000
	if seconds == 0 {
		return msgs.WorkedUnderSecond()
	}
	var elapsed string
	switch {
	case seconds >= 3600:
		elapsed = msgs.WorkHoursMinutesSeconds(int(seconds/3600), int(seconds%3600/60), int(seconds%60))
	case seconds >= 60:
		elapsed = msgs.WorkMinutesSeconds(int(seconds/60), int(seconds%60))
	default:
		elapsed = msgs.WorkSeconds(int(seconds))
	}
	return msgs.WorkedFor(elapsed)
}
