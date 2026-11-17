package hubui

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

func TestKeyboardResizeIntermediateOffsetsDoNotLeaveLatest(t *testing.T) {
	for _, layoutFirst := range []bool{false, true} {
		a := messageScrollAnchor{following: true}
		a.layout(core.Size{Width: 390, Height: 600})
		for _, height := range []core.Points{300, 420, 600} {
			size := core.Size{Width: 390, Height: height}
			if layoutFirst && !a.layout(size) {
				t.Fatal("resize did not restore the bottom")
			}
			// Native clamping can report a position away from the bottom
			// before the anchoring scroll reaches the platform.
			a.scroll(core.ScrollEvent{Size: size, ContentSize: core.Size{Height: 2000}, Offset: core.Point{Y: 1100}})
			if !a.following {
				t.Fatal("keyboard resize was mistaken for reading history")
			}
			a.layout(size)
			a.scroll(core.ScrollEvent{Size: size, ContentSize: core.Size{Height: 2000}, Offset: core.Point{Y: 2000 - height}})
			if a.restoring || !a.following {
				t.Fatal("bottom restoration did not finish")
			}
		}
		// A real drag interrupts restoration immediately.
		a.layout(core.Size{Width: 390, Height: 300})
		a.dragging, a.restoring = true, false
		a.scroll(core.ScrollEvent{Size: a.size, ContentSize: core.Size{Height: 2000}, Offset: core.Point{Y: 900}})
		if a.following || a.layout(core.Size{Width: 390, Height: 600}) {
			t.Fatal("resize pulled the reader out of history")
		}
	}
}
