package hubui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func (s *State) brand(cx *core.Scope, size core.Points, icon bool) core.Node {
	title := label(cx, "ReignHub", size, false)
	title.Style.FontWeight = core.FontWeightBold
	children := []core.Node{title}
	if icon {
		children = append([]core.Node{glyph(cx, lucide.Command, 22)}, children...)
	}
	brand := core.View{TestID: "brand-home", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: children}
	if isWeb() {
		brand.Role = core.RoleButton
		brand.Label = "ReignHub 官网"
		brand.OnPress = func(core.PressEvent) { openWebsite() }
		brand.Style.FocusVisible = &core.Style{OutlineWidth: 2, OutlineColor: palette(cx).mutedText}
	}
	return brand
}

// Overlays receive Back before this handler. Compact layouts then let the
// native page stack pop; wider layouts apply the same chat → sessions → devices
// hierarchy. The workspace root consumes Back instead of leaving the app.
func (s *State) workspaceBack() bool {
	if s.phoneNav != nil {
		return s.phoneNav.Depth() == 0
	}
	if s.narrowChat {
		s.backSessions()
	} else if s.target != "" {
		s.selectDevice("")
	}
	return true
}
