package hubui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

const windowBarHeight core.Points = 34

func customWindow(cx *core.Scope) bool {
	return !isWeb() && desktop.IsAvailable(cx)
}

func (s *State) windowFrame(cx *core.Scope, content core.Node) core.Node {
	if !customWindow(cx) {
		return content
	}
	desktop.SetTitleBar(cx, desktop.TitleBarHidden, windowBarHeight)
	desktop.SetWindowControls(cx, desktop.WindowControls{Hidden: true})
	desktop.SetMenuBar(cx, desktop.MenuBarAutoHide)
	control := func(id, title string, icon func() *core.SVG, action func()) core.Node {
		return core.View{TestID: id, Role: core.RoleButton, Label: title, Tooltip: label(cx, title, 12, false), Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}, OnPress: func(core.PressEvent) { action() }, Style: core.Style{Width: core.Pt(44), Height: core.Pt(windowBarHeight), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, FlexShrink: 0}, StyleFunc: func() core.Style {
			c := palette(cx)
			st := core.Style{Color: c.mutedText, Hover: &core.Style{Background: c.selected, Color: c.foreground}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: c.accent}}
			if id == "window-close" {
				st.Hover = &core.Style{Background: core.RGB(0xd9404b), Color: core.RGB(0xffffff)}
			}
			return st
		}, Children: []core.Node{core.Icon{SVG: icon(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14)}}}}
	}
	bar := core.View{TestID: "window-toolbar", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn}, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Height: core.Pt(windowBarHeight), FlexShrink: 0}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).canvas} }, Children: []core.Node{
		core.View{TestID: "window-drag-region", Style: core.Style{FlexGrow: 1, Height: core.Pct(100)}, Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn}},
		control("window-minimize", "最小化", lucide.Minus, func() { desktop.MinimizeWindow(cx) }),
		core.Dynamic(func(cx *core.Scope) core.Node {
			maximized := desktop.Maximized(cx)
			return control("window-maximize", choose(maximized, "还原窗口", "最大化"), choose(maximized, lucide.Copy, lucide.Square), func() { desktop.ToggleMaximize(cx) })
		}),
		control("window-close", "关闭窗口", lucide.X, func() { desktop.CloseWindow(cx) }),
	}}
	return core.View{TestID: "window-frame", Style: fill, Children: []core.Node{core.Show{When: func() bool { return !desktop.FullScreen(cx) }, Then: func(*core.Scope) core.Node { return bar }}, content}}
}
