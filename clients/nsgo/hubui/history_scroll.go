package hubui

import (
	"context"

	"github.com/monkeyWie/reignhub/pkg/persistence"
	"github.com/nsgo-dev/nsgo/core"
)

type historyEdge struct {
	before, after  persistence.PageCursor
	cursor         string
	local, reading bool
}

func (s *State) historyEdge() historyEdge {
	v := s.historyView()
	e := historyEdge{before: persistence.Origin(v), cursor: str(v, "nextCursor"), local: flag(v, "_localOlder"), reading: s.reading != nil}
	if n := len(s.readingPages); n > 0 {
		e.after = s.readingPages[n-1].End
	}
	return e
}

// The measured insets include folded groups and the empty space of an
// end-aligned List. A request belongs to one edge and one direction.
func (s *State) historyScroll(cx *core.Scope, a *messageScrollAnchor, inset core.Points) (core.View, core.View, func(), func(int)) {
	owner := cx.Context()
	var header, footer core.Ref
	type attempt struct {
		edge      historyEdge
		direction int
	}
	var attempted *attempt
	var direction int
	var queued, moving bool
	var cancel context.CancelFunc
	retry := core.NewSignal(cx, false)
	travel := core.NewSignal(cx, core.Points(0))
	pending := func() bool { return s.actionPending("history:" + s.key()) }
	request := func(edge historyEdge, d int) {
		req := attempt{edge, d}
		attempted = &req
		// Keep the gesture direction through the read. If sparse results
		// still leave the viewport at this edge, continue after layout.
		moving = d != 0
		if cancel != nil {
			cancel()
		}
		ctx, stop := context.WithCancel(owner)
		cancel = stop
		retry.Set(false)
		valid := func() bool {
			ok := s.timeline == a && s.historyEdge() == edge && (direction == d || d == 0 && a.following)
			if !ok {
				attempted = nil
			}
			return ok
		}
		if d > 0 {
			s.newerPage(ctx, valid)
		} else {
			s.loadOlder(ctx, d == 0, valid)
		}
	}
	check := func() {
		queued = false
		if s.timeline != a || pending() || !s.conversationVisible() || a.pendingRow != nil {
			return
		}
		edge := s.historyEdge()
		viewport, ok := a.list.Bounds()
		start, mounted := header.Bounds()
		end, hasEnd := footer.Bounds()
		if !ok || !mounted || !hasEnd || viewport.Size.Height <= 0 {
			return
		}
		h := viewport.Size.Height
		top := start.Origin.Y - viewport.Origin.Y
		bottom := end.Origin.Y - viewport.Origin.Y - h
		older := edge.local || edge.cursor != "" && s.connected()
		if attempted != nil && attempted.edge == edge && (attempted.direction == direction || attempted.direction == 0) {
			retry.Set(true)
			return
		}
		attempted = nil
		retry.Set(false)
		if moving && direction > 0 && edge.reading && bottom <= h*.75 {
			request(edge, 1)
			return
		}
		if moving && direction < 0 && older && !a.following && top >= -h*.75 {
			request(edge, -1)
			return
		}
		if edge.reading {
			// When every retained page folds into a few rows, keep enough
			// scroll range to report a gesture in either direction.
			rowHeight := end.Origin.Y - (start.Origin.Y + start.Size.Height)
			gap := h - inset - rowHeight
			if gap >= 0 {
				travel.Set(gap + 24)
			} else {
				travel.Set(0)
			}
			return
		}
		if !older {
			travel.Set(0)
			return
		}
		if extra := travel.GetUntracked(); extra > 0 {
			if top+extra < -h*.75 {
				travel.Set(0)
			}
			return
		}
		if top > 1 || top >= 0 && (len(s.messageRows) == 0 || a.following) {
			prefix := s.historyPrefix
			if len(prefix.pages) >= historyWindowPages-1 || prefix.bytes >= persistence.PageBytes {
				// Make very dense folded pages scrollable in both directions.
				travel.Set(top + 24)
				return
			}
			request(edge, 0)
		}
	}
	schedule := func() {
		if !queued {
			queued = true
			cx.Post(check)
		}
	}
	cx.Effect(func() { s.read(); schedule() })
	gesture := func(d int) {
		if d != direction && pending() && attempted != nil && attempted.direction != 0 && cancel != nil {
			cancel()
			attempted = nil
		}
		direction = d
		moving = true
		if retry.GetUntracked() {
			attempted = nil
			retry.Set(false)
		}
		schedule()
	}
	// A failed read can also be retried with a fresh scroll. The accessible
	// retry control only exists while that edge is in an error state.
	retryButton := func(d int) core.Node {
		return core.Show{When: func() bool { return retry.Get() && attempted != nil && (attempted.direction > 0) == (d > 0) }, Then: func(cx *core.Scope) core.Node {
			return button(cx, choose(d < 0, "load-older", "retry-newer"), "重试", nil, "ghost", func() {
				if attempted != nil && !pending() {
					request(s.historyEdge(), attempted.direction)
				}
			})
		}}
	}
	return core.View{TestID: "history-start-inset", Ref: &header, StyleFunc: func() core.Style { return core.Style{Padding: core.Edges{Top: inset + travel.Get()}} }, Children: []core.Node{retryButton(-1)}},
		core.View{Ref: &footer, StyleFunc: func() core.Style {
			s.read()
			if s.reading != nil && travel.Get() > 0 {
				return core.Style{Padding: core.Edges{Bottom: 24}}
			}
			return core.Style{}
		}, Children: []core.Node{retryButton(1)}}, schedule, gesture
}
