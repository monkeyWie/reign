package hubui

import "github.com/nsgo-dev/nsgo/core"

// Android's custom fling can bypass its stop handler when a clickable row
// takes ACTION_DOWN. Capture observes the touch without taking the gesture.
// Use the public scroll command to stop at the latest reported offset.
type listTouchBrake struct {
	enabled           bool
	ref               *core.Ref
	offset            core.Point
	momentum, stopTap bool
}

func (b *listTouchBrake) capture(e core.TouchEvent) {
	if !b.enabled {
		return
	}
	if e.Phase == core.TouchStart {
		b.stopTap = b.momentum
		if b.momentum {
			b.momentum = false
			b.ref.ScrollTo(b.offset, false)
		}
	} else if e.Phase == core.TouchCancel {
		b.stopTap = false
	}
}

func (b *listTouchBrake) action(run func()) func() {
	return func() {
		if b.stopTap {
			b.stopTap = false
			return
		}
		run()
	}
}

func (b *listTouchBrake) wrap(list core.List) core.Node {
	if !b.enabled {
		return list
	}
	b.ref = &core.Ref{}
	list.Ref = b.ref
	list.OnScroll = func(e core.ScrollEvent) { b.offset = e.Offset }
	list.OnMomentumStart = func(e core.ScrollEvent) { b.offset, b.momentum = e.Offset, true }
	list.OnMomentumEnd = func(e core.ScrollEvent) { b.offset, b.momentum = e.Offset, false }
	return core.View{TestID: "session-list-touch-surface", Style: fill, OnTouchCapture: b.capture, Children: []core.Node{list}}
}
