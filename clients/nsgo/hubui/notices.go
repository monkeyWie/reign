package hubui

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

const noticeDuration = 5 * time.Second

// Informational feedback belongs to the workspace, not to a recycled row or
// dialog. Replacing it restarts its lifetime; unrelated refreshes do not.
func (s *State) showNotice(message string) {
	if s.noticeTimer != nil {
		s.noticeTimer()
		s.noticeTimer = nil
	}
	s.noticeGeneration++
	generation := s.noticeGeneration
	s.notice = message
	s.changed()
	if message != "" {
		s.noticeTimer = s.cx.AfterFunc(noticeDuration, func() {
			if s.noticeGeneration == generation {
				s.noticeTimer = nil
				s.notice = ""
				s.changed()
			}
		})
	}
}

func (s *State) noticeOverlay(cx *core.Scope) core.Node {
	return core.Show{When: func() bool { s.read(); return s.notice != "" }, Then: func(cx *core.Scope) core.Node {
		text := dynamicText(cx, func() string { s.read(); return s.notice }, 12, false)
		text.Role = core.RoleStatus
		text.Style.FlexShrink, text.Style.MinWidth = 1, core.Pt(0)
		return core.Overlay{TestID: "notice-overlay", Topmost: true, PointerEvents: core.PointerEventsBoxNone,
			Style: core.Style{Left: core.Pt(0), Width: core.Pct(100), Bottom: core.Pt(16), Padding: core.Edges{Left: 18, Right: 18}, PaddingEnv: core.EnvEdges{Bottom: core.EnvSafeArea}, AlignItems: core.AlignEnd},
			Children: []core.Node{core.View{TestID: "notice", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MaxWidth: core.Pct(100), Padding: core.Symmetric(6, 10), CornerRadius: 8, BorderWidth: 1}, StyleFunc: func() core.Style {
				p := palette(cx)
				return core.Style{Background: p.background, BorderColor: p.border}
			}, Children: []core.Node{
				core.View{Style: core.Style{MinWidth: core.Pt(0), MaxWidth: core.Pt(440), FlexShrink: 1}, Children: []core.Node{text}},
				contextualIconButton(cx, "dismiss-notice", "关闭提示", lucide.X, func() { s.showNotice("") }),
			}}},
		}
	}}
}
