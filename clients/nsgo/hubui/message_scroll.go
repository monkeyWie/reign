package hubui

import "github.com/nsgo-dev/nsgo/core"

// A keyboard/composer resize reports intermediate offsets as the native scroll
// view clamps and the inverted list anchors. These are not a user leaving the
// latest message. Retain the prior intent until the bottom is restored, or a
// real drag takes over. The same resize while reading history leaves it alone.
type messageScrollAnchor struct {
	size      core.Size
	following bool
	restoring bool
	dragging  bool
}

func (a *messageScrollAnchor) layout(size core.Size) bool {
	previous := a.size
	a.size = size
	if size == previous || previous.Height <= 0 || size.Height <= 0 || !a.following || a.dragging {
		return false
	}
	a.restoring = true
	return true
}

func (a *messageScrollAnchor) scroll(e core.ScrollEvent) bool {
	// Some platforms deliver the resized scroll event before OnLayout.
	restore := a.layout(e.Size)
	distance := e.ContentSize.Height - e.Size.Height - e.Offset.Y
	if a.restoring {
		if distance <= 1 {
			a.restoring = false
		}
		return restore
	}
	a.following = distance < 48
	return restore
}
