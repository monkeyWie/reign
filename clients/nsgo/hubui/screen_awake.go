package hubui

import "github.com/nsgo-dev/nsgo/core"

// A selected conversation may remain cached underneath another page. Follow
// the visible route rather than the selection or the background-service state.
func (s *State) observeScreenAwake() {
	set := screenAwake(s.cx)
	mobileBrowser := false
	if isWeb() {
		platform := platformClientInfo().Platform
		mobileBrowser = platform == "ios" || platform == "android"
	}
	wanted := core.NewMemo(s.cx, func() bool {
		s.read()
		if !(isMobile(s.cx) || mobileBrowser) || s.cx.Env().Lifecycle() != core.LifecycleActive || s.selected == nil || s.target == "" {
			return false
		}
		width := s.width.Get()
		if width < compactBreakpoint {
			return s.phoneRoute.Get() == "chat:"+s.key()
		}
		return width >= wideBreakpoint || s.narrowChat
	})
	s.cx.Effect(func() { set(wanted.Get()) })
}
