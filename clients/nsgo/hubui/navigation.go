package hubui

import (
	"time"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/mobile"
)

func (s *State) brand(cx *core.Scope, size core.Points, icon bool) core.Node {
	title := label(cx, "ReignHub", size, false)
	title.Style.FontWeight = core.FontWeightBold
	children := []core.Node{title}
	if icon {
		children = append([]core.Node{glyph(cx, lucide.Command, 22)}, children...)
	}
	brand := core.View{TestID: "brand-home", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: children}
	if isWeb() && !s.api.CanHost() {
		brand.Role = core.RoleButton
		brand.Label = "ReignHub 官网"
		brand.OnPress = func(core.PressEvent) { openWebsite() }
		brand.StyleFunc = func() core.Style {
			return core.Style{FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: palette(cx).focus}}
		}
	} else if s.api.CanHost() {
		brand.Role, brand.Label = core.RoleButton, "ReignHub · "+locales.For(cx).WorkspaceHome()
		brand.Tooltip = tooltipCard(cx, label(cx, locales.For(cx).WorkspaceHome(), 12, false))
		brand.OnPress = func(core.PressEvent) { s.showHome() }
		brand.StyleFunc = func() core.Style {
			return core.Style{Hover: &core.Style{Opacity: core.Alpha(.8)}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: palette(cx).focus}}
		}
	}
	return brand
}

// Overlays receive Back first. Compact content is the workspace root, with
// Android's second-Back confirmation; it never pops a conversation route.
func (s *State) workspaceBack() bool {
	if s.mobileSidebarOpen.GetUntracked() {
		s.closeMobileSidebar()
		return true
	}
	if s.narrowLayout() {
		return s.rootBack()
	}
	if s.selected != nil {
		s.showHome()
	}
	return true
}

const rootBackHint = "再返回一次退出应用"
const rootBackInterval = 2 * time.Second

func (s *State) clearRootBack() {
	s.rootBackDeadline = time.Time{}
	if s.notice == rootBackHint {
		s.showNotice("")
	}
}

func (s *State) rootBack() bool {
	if isWeb() || s.cx.Env().OS() != core.OSAndroid {
		return true
	}
	now := s.cx.Now()
	if now.Before(s.rootBackDeadline) {
		s.clearRootBack()
		mobile.ExitApp(s.cx)
		return true
	}
	deadline := now.Add(rootBackInterval)
	s.rootBackDeadline = deadline
	s.showNotice(rootBackHint)
	s.cx.AfterFunc(rootBackInterval, func() {
		if s.rootBackDeadline == deadline {
			s.clearRootBack()
		}
	})
	return true
}
