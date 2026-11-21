package hubui

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Keep the authoritative transcript untouched. Only new live suffixes are
// revealed over a short interval; history, corrections and catch-up are instant.
func smoothReply(cx *core.Scope, text func() string, animate func() bool) func() string {
	// A recycled row may mount with a complete or already streaming reply.
	// Layout and draw that text immediately, then animate subsequent deltas.
	shown := core.NewSignal(cx, core.Untracked(cx, text))
	type input struct {
		text    string
		animate bool
	}
	// State revisions also include connection/catalogue/approval changes. They
	// must not dispose and restart the text animation when its inputs match.
	source := core.NewMemo(cx, func() input { return input{text(), animate() && !ui.ReduceMotion(cx)} })
	var stop func()
	cx.Cleanup(func() {
		if stop != nil {
			stop()
		}
	})
	cx.Effect(func() {
		value := source.Get()
		target, enabled := value.text, value.animate
		current := shown.GetUntracked()
		if stop != nil {
			stop()
			stop = nil
		}
		if !enabled || !strings.HasPrefix(target, current) || target == current {
			shown.Set(target)
			return
		}
		suffix := []rune(strings.TrimPrefix(target, current))
		duration := min(180*time.Millisecond, time.Duration(len(suffix))*18*time.Millisecond)
		if duration <= 0 {
			shown.Set(target)
			return
		}
		last := 0
		stop = cx.Animate(duration, core.Linear, func(progress float64) {
			n := min(len(suffix), int(float64(len(suffix))*progress))
			if n == last && progress < 1 {
				return
			}
			// Never split an encoded character, including CJK text and emoji.
			end := len(current)
			for i := 0; i < n; i++ {
				_, size := utf8.DecodeRuneInString(target[end:])
				end += size
			}
			shown.Set(target[:end])
			last = n
		})
	})
	return shown.Get
}

func (s *State) syncIndicator(cx *core.Scope) core.Node {
	return core.Show{When: func() bool {
		s.read()
		return s.transcript != nil && s.syncVisible && s.transcriptSyncing && s.connected()
	}, Then: func(cx *core.Scope) core.Node {
		dots := []core.Node{}
		for i := 0; i < 3; i++ {
			opacity := core.NewAnimatedValue(cx, 1)
			if !ui.ReduceMotion(cx) {
				cx.StartAnimation(core.ValueKeyframes(opacity, core.KeyframeTiming{Duration: 1100 * time.Millisecond, Delay: time.Duration(i) * 150 * time.Millisecond, Iterations: core.Forever}, core.StopAt(0, .25), core.StopAt(.35, 1), core.StopAt(.7, .25), core.StopAt(1, .25)), nil)
			}
			dots = append(dots, core.View{Style: core.Style{Width: core.Pt(5), Height: core.Pt(5), CornerRadius: 3}, StyleFunc: func() core.Style {
				return core.Style{Background: palette(cx).mutedText, Animated: &core.AnimatedStyle{Opacity: opacity}}
			}})
		}
		return core.View{TestID: "transcript-syncing", Role: core.RoleGroup, Label: "正在同步消息", PointerEvents: core.PointerEventsNone, Style: core.Style{Position: core.Absolute, Bottom: core.Pt(0), Left: core.Pt(0), Right: core.Pt(0), Height: core.Pt(22), ZIndex: 2, Direction: core.Row, Gap: 5, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}, Children: dots}
	}}
}
