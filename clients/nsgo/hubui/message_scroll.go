package hubui

import "github.com/nsgo-dev/nsgo/core"

// A keyboard/composer resize reports intermediate offsets as the native scroll
// view clamps and the inverted list anchors. These are not a user leaving the
// latest message. Retain the prior intent until the bottom is restored, or a
// real drag takes over. The same resize while reading history leaves it alone.
type messageScrollAnchor struct {
	size      core.Size
	content   core.Size
	following bool
	restoring bool
	dragging  bool
}

func (a *messageScrollAnchor) layout(size core.Size) bool {
	if size.Height <= 0 {
		return false
	}
	previous := a.size
	a.size = size
	if size == previous || !a.following || a.dragging {
		return false
	}
	a.restoring = true
	return true
}

func (a *messageScrollAnchor) scroll(e core.ScrollEvent) bool {
	// Native page transitions can publish offsets before a usable viewport.
	// They cannot establish whether the user is reading history.
	if e.Size.Height <= 0 {
		return false
	}
	// Some platforms deliver the resized scroll event before OnLayout.
	restore := a.layout(e.Size)
	// Initial history, streaming prefixes and image/Markdown measurements can
	// report an old offset against new content before native anchoring lands.
	// Preserve intent and explicitly pin row zero instead of showing the jump
	// control for those intermediate layout events.
	if a.content != e.ContentSize {
		a.content = e.ContentSize
		if a.following && !a.dragging && e.Size.Height > 0 {
			a.restoring, restore = true, true
		}
	}
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
