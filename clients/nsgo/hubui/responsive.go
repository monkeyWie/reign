package hubui

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/core"
)

// Layout depends only on the available viewport, never the platform or OS.
// Compact uses a page stack; medium keeps the device sidebar; wide also keeps
// the session directory next to the conversation.
const (
	compactBreakpoint core.Points = 650
	wideBreakpoint    core.Points = 1000
)

func (s *State) narrowLayout() bool { return s.width.Get() < wideBreakpoint }

// Card collections respond to their container, independently of page/sidebar
// breakpoints. Clamp the track minimum below the preferred card width so a
// narrow parent never overflows. NSGo grid tracks do not yet parse CSS min().
func cardGrid(cx *core.Scope, id string, minimum, gap core.Points, children ...core.Node) core.Node {
	width := core.NewSignal(cx, core.Points(0))
	return core.View{TestID: id, Style: core.Style{Display: core.DisplayGrid, Width: core.Pct(100), MinWidth: core.Pt(0), Gap: gap},
		OnLayout: func(b core.Bounds) { width.Set(b.Size.Width) },
		StyleFunc: func() core.Style {
			available := width.Get()
			if available <= 0 {
				return core.Style{GridTemplateColumns: "minmax(0, 1fr)"}
			}
			track := max(1, min(int(minimum), int(available)))
			return core.Style{GridTemplateColumns: core.GridTracks(fmt.Sprintf("repeat(auto-fill, minmax(%dpx, 1fr))", track))}
		}, Children: children}
}
