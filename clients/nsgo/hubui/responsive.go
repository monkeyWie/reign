package hubui

import "github.com/nsgo-dev/nsgo/core"

// Layout depends only on the available viewport, never the platform or OS.
// Compact uses a page stack; medium keeps the device sidebar; wide also keeps
// the session directory next to the conversation.
const (
	compactBreakpoint core.Points = 650
	wideBreakpoint    core.Points = 1000
)

func (s *State) narrowLayout() bool { return s.width.Get() < wideBreakpoint }
