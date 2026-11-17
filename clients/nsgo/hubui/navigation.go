package hubui

import (
	"time"

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
// hierarchy. Android's workspace root requires a second Back to leave.
func (s *State) workspaceBack() bool {
	if s.phoneNav != nil {
		if s.phoneNav.Depth() > 0 {
			s.clearRootBack()
			return false
		}
		return s.rootBack()
	}
	if s.narrowChat {
		s.backSessions()
	} else if s.target != "" {
		s.selectDevice("")
	}
	return true
}

const rootBackHint = "再返回一次退出应用"
const rootBackInterval = 2 * time.Second

func (s *State) clearRootBack() {
	s.rootBackDeadline = time.Time{}
	if s.notice == rootBackHint {
		s.notice = ""
		s.changed()
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
	s.rootBackDeadline, s.notice = deadline, rootBackHint
	s.changed()
	s.cx.AfterFunc(rootBackInterval, func() {
		if s.rootBackDeadline == deadline {
			s.clearRootBack()
		}
	})
	return true
}
