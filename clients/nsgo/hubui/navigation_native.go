//go:build !js

package hubui

func (s *State) installWorkspaceNavigation() {
	if s.cx.Env().OS() == "android" {
		s.cx.OnBackPress(s.workspaceBack)
	}
}
func openWebsite() {}
