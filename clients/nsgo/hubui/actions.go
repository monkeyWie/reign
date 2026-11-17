package hubui

import (
	"context"
	"errors"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Action state belongs to the operation, not the page or a global UI lock.
// Writes continue if their initiating control unmounts; they are never replayed.
func (s *State) actionPending(key string) bool {
	s.read()
	return s.actions[key]
}

func (s *State) runAction(key string, work func(context.Context) (any, error), done func(any), current ...func() bool) {
	if s.ctx.Err() != nil || s.actionPending(key) {
		return
	}
	if s.actions == nil {
		s.actions = map[string]bool{}
	}
	s.actions[key] = true
	s.error, s.failure = "", nil
	s.changed()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		v, err := work(ctx)
		s.cx.Post(func() {
			delete(s.actions, key)
			if s.ctx.Err() != nil {
				return
			}
			valid := len(current) == 0 || current[0]()
			if valid && err != nil {
				s.error, s.failure = err.Error(), nil
				_ = errors.As(err, &s.failure)
			} else if valid && done != nil {
				done(v)
			}
			s.changed()
		})
	}()
}

func (s *State) actionButton(cx *core.Scope, id, title string, icon func() *core.SVG, variant string, onPress func()) core.Node {
	return busyButton(cx, id, title, icon, variant, func() bool { return s.actionPending(id) }, onPress)
}

func busyButton(cx *core.Scope, id, title string, icon func() *core.SVG, variant string, pending func() bool, onPress func()) core.Node {
	busy := core.NewMemo(cx, pending)
	v := button(cx, id, title, icon, variant, onPress).(core.View)
	v.Busy, v.Disabled = busy.Get, busy.Get
	// Keep the real content's bounds while the spinner replaces it. An empty
	// leading icon slot would offset text-only labels from the button's center.
	v.Children = []core.Node{core.View{
		TestID:    id + "-content",
		Style:     core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 8, MinWidth: core.Pt(16), MinHeight: core.Pt(18), FlexShrink: 1},
		StyleFunc: func() core.Style { return core.Style{Opacity: core.Alpha(choose(busy.Get(), float32(0), float32(1)))} },
		Children:  v.Children,
	}, core.View{
		PointerEvents: core.PointerEventsNone,
		Style:         core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Top: core.Pt(0), Bottom: core.Pt(0), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
		Children: []core.Node{core.Show{When: busy.Get, Then: func(cx *core.Scope) core.Node {
			return actionSpinner(cx, id+"-loading", title, variant == "primary", 16)
		}}},
	}}
	return v
}

func actionSpinner(cx *core.Scope, id, title string, inverse bool, size core.Points) core.Node {
	if inverse {
		theme := *cx.Theme()
		theme.Light.TextMuted, theme.Dark.TextMuted = theme.Light.AccentText, theme.Dark.AccentText
		cx.SetTheme(&theme)
	}
	return ui.Spinner{TestID: id, Label: title, Variant: ui.SpinnerSpokes, Size: size}
}

func pressStyle(cx *core.Scope) core.Style {
	c := palette(cx)
	return core.Style{Hover: &core.Style{Background: c.muted}, Active: &core.Style{Background: c.selected, Opacity: core.Alpha(.8)}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineColor: c.accent}}
}

func (s *State) approvalKey(a Object) string { return "approval:" + s.key() + ":" + str(a, "id") }

func (s *State) approvalAction(a, decision Object) {
	key, target, p, id := s.key(), s.target, str(s.selected, "providerId"), str(s.selected, "id")
	s.runAction(s.approvalKey(a), func(ctx context.Context) (any, error) {
		return rpc(ctx, s.api, target, p, id, "decide", Object{"approval": a["id"], "decision": decision})
	}, func(any) {
		kept := []Object{}
		for _, existing := range s.approvals {
			if str(existing, "id") != str(a, "id") {
				kept = append(kept, existing)
			}
		}
		s.approvals = kept
	}, func() bool { return s.key() == key })
}

func labelledIconButton(cx *core.Scope, id, title string, icon func() *core.SVG, onPress func()) core.Node {
	v := button(cx, id, "", icon, "ghost", onPress).(core.View)
	v.Label, v.Tooltip = title, label(cx, title, 12, false)
	return v
}
