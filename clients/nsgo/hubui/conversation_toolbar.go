package hubui

import (
	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

// The conversation heading shares the window toolbar. Only a session change
// replaces its controls; catalogue refreshes update labels without closing details.
func (s *State) contentToolbarSlot(cx *core.Scope) core.Node {
	selection := core.NewMemo(cx, func() string {
		s.read()
		if s.selected == nil {
			return ""
		}
		return s.key()
	})
	return core.View{TestID: "content-toolbar-slot", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
		Style: fill.Merge(core.Style{Height: core.Pct(100), Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Edges{Left: 12, Right: 12}, Margin: core.Edges{Left: 5}}),
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			if selection.Get() == "" {
				return nil
			}
			return core.Untracked(cx, func() core.Node { return s.conversationToolbar(cx) })
		})},
	}
}

func (s *State) conversationToolbar(cx *core.Scope) core.Node {
	cache := labelledIconButton(cx, "attachment-cache", "管理附件缓存", lucide.HardDrive, func() { s.dialog.Set("clear-cache") }).(core.View)
	cache.Desktop.WindowDrag = core.WindowDragOff
	cache.Style.Width, cache.Style.Height, cache.Style.MinHeight, cache.Style.Padding = core.Pt(32), core.Pt(30), core.Pt(30), core.All(7)
	return core.View{TestID: "conversation-header", Style: fill.Merge(core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Height: core.Pct(100)}),
		Children: []core.Node{
			core.View{TestID: "conversation-title-control", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}, Style: core.Style{MaxWidth: core.Pt(360), MinWidth: core.Pt(0), FlexShrink: 1}, Children: []core.Node{s.conversationDetails(cx)}},
			spacer(),
			core.View{TestID: "conversation-toolbar-actions", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 4, FlexShrink: 0}, Children: []core.Node{s.conversationConnectionIndicator(cx), s.terminalButton(cx), cache}},
		},
	}
}

// Local execution has no remote transport. Host connectivity belongs to the
// device overview, not to a local conversation's desktop or compact header.
func (s *State) conversationConnectionIndicator(cx *core.Scope) core.Node {
	return core.Show{When: func() bool {
		s.read()
		return s.target != "" && s.target != control.LocalTarget
	}, Then: func(cx *core.Scope) core.Node {
		return s.connectionIndicator(cx, func() string { return s.target })
	}}
}

func (s *State) conversationDetails(cx *core.Scope) core.Node {
	open := core.NewSignal(cx, false)
	title := func() string {
		s.read()
		if name := str(s.selected, "title"); name != "" {
			return name
		}
		return locales.For(cx).UnnamedSession()
	}
	heading := dynamicText(cx, title, 14, false)
	heading.Selectable, heading.MaxLines = core.SelectableOn, 4
	return base.Popover{TestID: "conversation-details", Label: locales.For(cx).SessionInformation(), Open: open.Get,
		OnOpenChange: func(value bool, _ base.Reason) { open.Set(value) }, Placement: core.PlaceBottom, Align: core.AlignStart, Offset: 7, Margin: 8,
		Style:      core.Style{MinWidth: core.Pt(0), FlexShrink: 1, Height: core.Pt(30), CornerRadius: 7, Padding: core.Symmetric(5, 8), JustifyContent: core.JustifyCenter},
		StyleFunc:  func() core.Style { return pressStyle(cx) },
		Trigger:    row(glyph(cx, lucide.FolderClosed, 14), singleLine(cx, title, 13, false), glyph(cx, lucide.ChevronDown, 12)),
		PopupStyle: core.Style{Padding: core.All(16), Gap: 14, CornerRadius: 12, BorderWidth: 1},
		PopupStyleFunc: func() core.Style {
			c := palette(cx)
			return core.Style{Width: core.Pt(min(360, max(160, cx.Env().Window().Width-24))), Background: c.elevated, Color: c.foreground, BorderColor: c.border,
				Shadows: []core.BoxShadow{{Offset: core.Point{Y: 6}, Blur: 20, Color: core.RGB(0).Faded(.16)}}}
		},
		Children: []core.Node{core.View{TestID: "conversation-info-panel", Style: core.Style{Gap: 14, MinWidth: core.Pt(0)}, Children: []core.Node{
			heading,
			row(s.hostGlyph(cx, s.target, 16), singleLine(cx, func() string { s.read(); return s.environmentName(s.target) }, 13, true)),
			row(agentGlyph(cx, str(s.selected, "providerId"), 16), singleLine(cx, func() string { s.read(); return s.providerName(s.target, str(s.selected, "providerId")) }, 13, true)),
			core.Show{When: func() bool { s.read(); return projectDirectory(s.selected).Path != "" }, Then: func(cx *core.Scope) core.Node {
				return core.View{Style: core.Style{Gap: 5}, Children: []core.Node{label(cx, locales.For(cx).Projects(), 11, true),
					singleLine(cx, func() string { s.read(); return projectDirectory(s.selected).Title }, 13, false),
				}}
			}},
			core.Show{When: func() bool { s.read(); return str(s.selected, "cwd") != "" }, Then: func(cx *core.Scope) core.Node {
				text := dynamicText(cx, func() string { s.read(); return str(s.selected, "cwd") }, 12, false)
				text.Selectable, text.MaxLines = core.SelectableOn, 4
				return core.View{Style: core.Style{Gap: 5}, Children: []core.Node{label(cx, locales.For(cx).WorkingDirectory(), 11, true), text}}
			}},
		}}},
	}
}
