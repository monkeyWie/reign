package hubui

import "github.com/nsgo-dev/nsgo/core"

// The navigation drawer covers the conversation without stopping its watch.
// Only the unobscured conversation holds the display awake.
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
		return s.conversationVisible()
	})
	s.cx.Effect(func() { set(wanted.Get()) })
}
