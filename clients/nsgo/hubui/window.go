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

	// Paint the title bar ourselves while the platform keeps its own controls:
	// traffic lights on macOS and caption buttons on Windows. GTK removes
	// its controls with the decorations, so Linux gets GNOME-style controls.
	desktop.SetTitleBar(cx, desktop.TitleBarHidden, windowBarHeight)
	desktop.SetWindowControls(cx, desktop.WindowControls{})
	desktop.SetMenuBar(cx, desktop.MenuBarAutoHide)
	bar := core.View{
		TestID:  "window-toolbar",
		Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
		StyleFunc: func() core.Style {
			insets := desktop.TitleBar(cx)
			return core.Style{
				Direction: core.Row, AlignItems: core.AlignCenter, FlexShrink: 0,
				Height:     core.Pt(max(windowBarHeight, insets.Top)),
				Padding:    core.Edges{Left: insets.Left, Right: insets.Right},
				Background: palette(cx).canvas,
			}
		},
		Children: []core.Node{core.View{
			TestID:  "window-drag-region",
			Style:   core.Style{FlexGrow: 1, Height: core.Pct(100)},
			Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
		}},
	}
	if cx.Env().OS() == core.OSLinux {
		bar.Children = append(bar.Children, linuxWindowControls(cx)...)
	}
	return core.View{TestID: "window-frame", Style: fill, Children: []core.Node{
		core.Show{When: func() bool { return !desktop.FullScreen(cx) }, Then: func(*core.Scope) core.Node { return bar }},
		content,
	}}
}

// Linux's hidden decorations contain no buttons in NSGo. Keep the familiar
// circular header buttons without restoring a separate system title bar.
func linuxWindowControls(cx *core.Scope) []core.Node {
	control := func(id, title string, icon func() *core.SVG, action func()) core.Node {
		return core.View{
			TestID: id, Role: core.RoleButton, Label: title,
			Tooltip: label(cx, title, 12, false),
			Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff},
			OnPress: func(core.PressEvent) { action() },
			Style:   core.Style{Width: core.Pt(28), Height: core.Pt(28), Margin: core.Edges{Right: 6}, CornerRadius: 14, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, FlexShrink: 0},
			StyleFunc: func() core.Style {
				c := palette(cx)
				return core.Style{Color: c.foreground, Background: c.selected, Hover: &core.Style{Background: c.border}, Active: &core.Style{Opacity: core.Alpha(.65)}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: c.accent}}
			},
			Children: []core.Node{core.Icon{SVG: icon(), Style: core.Style{Width: core.Pt(14), Height: core.Pt(14)}}},
		}
	}
	return []core.Node{
		control("window-minimize", "最小化", lucide.Minus, func() { desktop.MinimizeWindow(cx) }),
		core.Dynamic(func(cx *core.Scope) core.Node {
			maximized := desktop.Maximized(cx)
			return control("window-maximize", choose(maximized, "还原窗口", "最大化"), choose(maximized, lucide.Copy, lucide.Square), func() { desktop.ToggleMaximize(cx) })
		}),
		control("window-close", "关闭窗口", lucide.X, func() { desktop.CloseWindow(cx) }),
	}
}
