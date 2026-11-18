package hubui

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// One platform animation drives every mounted ring in this workspace. New or
// recycled rows join its current angle instead of starting another revolution.
type loadingMotion struct {
	rotation *core.AnimatedValue
	users    *core.Signal[int]
}

func newLoadingMotion(cx *core.Scope) *loadingMotion {
	m := &loadingMotion{rotation: core.NewAnimatedValue(cx, 0), users: core.NewSignal(cx, 0)}
	active := core.NewMemo(cx, func() bool { return m.users.Get() > 0 && !cx.Env().ReduceMotion() })
	cx.Effect(func() {
		if active.Get() {
			// The effect owns this run: no rings, reduced motion, or workspace
			// disposal stops it. Changing the number of active rings does not.
			cx.StartAnimation(core.ValueKeyframes(m.rotation, core.KeyframeTiming{Duration: 1400 * time.Millisecond, Iterations: core.Forever}, core.Stops(0, 360)...), nil)
		} else {
			m.rotation.Set(0)
		}
	})
	return m
}

type loadingRing struct {
	motion    *loadingMotion
	id, label string
	size      core.Points
}

func (s *State) loadingRing(id, label string, size core.Points) core.Node {
	return loadingRing{motion: s.loadingMotion, id: id, label: label, size: size}
}

func (r loadingRing) Render(cx *core.Scope) core.Node {
	r.motion.users.Update(func(n int) int { return n + 1 })
	cx.Cleanup(func() { r.motion.users.Update(func(n int) int { return n - 1 }) })
	// Preserve NSGo's indeterminate progress semantics and ring geometry, with
	// neutral ink and the shared native rotation binding.
	return base.Progress{TestID: r.id, Label: r.label, Indeterminate: func() bool { return true },
		TrackStyle: core.Style{Display: core.DisplayNone},
		Style:      core.Style{Width: core.Pt(r.size), Height: core.Pt(r.size), FlexShrink: 0, Animated: &core.AnimatedStyle{Rotate: r.motion.rotation}},
		Children: []core.Node{core.Canvas{Style: core.Style{FlexGrow: 1}, Draw: func(d *core.Drawing, size core.Size) {
			diameter := float32(min(size.Width, size.Height))
			if diameter <= 0 {
				return
			}
			c := palette(cx)
			width := min(diameter/10, 3)
			radius := (diameter - width) / 2
			x, y := float32(size.Width)/2, float32(size.Height)/2
			d.SetLineWidth(width).SetLineCap(core.CapRound)
			d.BeginPath().Arc(x, y, radius, 0, 2*math.Pi, false).SetStrokeColor(c.border).Stroke()
			d.BeginPath().Arc(x, y, radius, -math.Pi/2, 0, false).SetStrokeColor(c.mutedText).Stroke()
		}}}}
}
