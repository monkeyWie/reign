package hubui

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

func installScrollbars(cx *core.Scope) {
	thumb := func() core.Style {
		return core.Style{Background: palette(cx).mutedText.Faded(.45)}
	}
	base.SetScrollbarTheme(cx, &base.ScrollbarTheme{
		Mode:           base.ScrollbarScrolling,
		Motion:         base.ScrollbarMotion{Idle: 700, Enter: 100, Exit: 180},
		MinThumbLength: 32,
		Vertical: &base.ScrollbarLook{
			Label:      "垂直滚动条",
			Style:      core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Right: core.Pt(0), Width: core.Pt(8), Padding: core.All(2)},
			ThumbStyle: core.Style{CornerRadius: 2}, ThumbStyleFunc: thumb,
		},
		Horizontal: &base.ScrollbarLook{
			Label:      "水平滚动条",
			Style:      core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Height: core.Pt(8), Padding: core.All(2)},
			ThumbStyle: core.Style{CornerRadius: 2}, ThumbStyleFunc: thumb,
		},
	})
}
