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
			if layoutFirst {
				a.layout(size)
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
		a.layout(core.Size{Width: 390, Height: 600})
		if a.following || a.restoring {
			t.Fatal("resize pulled the reader out of history")
		}
	}
}

func TestInitialMeasurementsAndProgressiveContentKeepLatestIntent(t *testing.T) {
	a := messageScrollAnchor{following: true}
	a.scroll(core.ScrollEvent{ContentSize: core.Size{Height: 2400}})
	if !a.following {
		t.Fatal("unlaid page lost latest intent")
	}
	size := core.Size{Width: 390, Height: 600}
	a.layout(size)
	if !a.following {
		t.Fatal("first viewport must keep latest intent")
	}
	// Android can report the unadjusted offset while first history and measured
	// Markdown replace the empty/estimated list. Neither event is user input.
	for _, height := range []core.Points{900, 2400, 3600} {
		a.scroll(core.ScrollEvent{Size: size, ContentSize: core.Size{Height: height}, Offset: core.Point{Y: 0}})
		if !a.following {
			t.Fatal("initial content was mistaken for a history scroll")
		}
		a.scroll(core.ScrollEvent{Size: size, ContentSize: core.Size{Height: height}, Offset: core.Point{Y: height - size.Height}})
	}
	// An actual user drag must win, including when another row measures.
	a.dragging = true
	a.restoring = false
	a.scroll(core.ScrollEvent{Size: size, ContentSize: core.Size{Height: 3800}, Offset: core.Point{Y: 900}})
	a.dragging = false
	a.scroll(core.ScrollEvent{Size: size, ContentSize: core.Size{Height: 4000}, Offset: core.Point{Y: 900}})
	if a.following || a.restoring {
		t.Fatal("measurement pulled a reader out of history")
	}
}

func TestStaleScrollCannotUndoHistoryNavigation(t *testing.T) {
	a := messageScrollAnchor{size: core.Size{Width: 900, Height: 500}, content: core.Size{Height: 2000}}
	// An old bottom report may arrive after an explicit history jump. It is
	// still awaiting the framework's pending scroll and is not user intent.
	a.scroll(core.ScrollEvent{Size: a.size, ContentSize: a.content, Offset: core.Point{Y: 1500}, Stale: true})
	if a.following {
		t.Fatal("stale bottom report undid history navigation")
	}
	a.scroll(core.ScrollEvent{Size: a.size, ContentSize: a.content, Offset: core.Point{Y: 900}})
	if a.following || a.restoring {
		t.Fatal("settled history position changed latest intent")
	}
}
