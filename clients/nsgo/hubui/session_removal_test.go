package hubui

import (
	"context"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type sessionRemovalFixture struct {
	*demoAPI
	calls  chan string
	result chan error
}

func (f *sessionRemovalFixture) Call(ctx context.Context, method string, params Object, target string, request Object) (any, error) {
	if method == "client.call" && str(request, "method") == "providers" {
		// These tests explicitly supply each directory reconciliation.
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if method == "client.call" && str(request, "method") == "session.remove" {
		f.calls <- target + "/" + str(request, "provider") + "/" + str(request, "session")
		select {
		case err := <-f.result:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.demoAPI.Call(ctx, method, params, target, request)
}

func TestSessionRemovalRoutesAndPreservesOtherConversation(t *testing.T) {
	f := &sessionRemovalFixture{newDemoAPI(), make(chan string, 4), make(chan error, 4)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		s.api = f
		for _, ss := range s.sessions {
			ss["status"] = "idle"
		}
		s.target, s.selected = "remote", s.sessions[1]
		s.draft.Set("keep this draft")
		return core.View{Style: fill, Children: []core.Node{s.directory(cx), core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) })}}
	}, nsgotest.Size(800, 1000), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop, OS: core.OSMacOS}))
	remove := "id:" + sessionRowID("pinned", s.sessions[2])
	app.RightClick(remove)
	app.Press("id:session-remove-action")
	app.WaitFor("id:confirm-remove-session", time.Second)
	app.Press("id:cancel-remove-session")
	if len(f.calls) != 0 {
		t.Fatal("dismissal submitted removal")
	}
	app.RightClick(remove)
	app.Press("id:session-remove-action")
	app.Press("id:confirm-remove-session")
	app.WaitUntil(time.Second, func() bool { return len(f.calls) == 1 })
	if route := <-f.calls; route != "local/pi/design" {
		t.Fatal("wrong removal route", route)
	}
	if !app.Find("id:confirm-remove-session").Props.Busy {
		t.Fatal("removal has no pending feedback")
	}
	app.Screenshot("session-removal-confirmation")
	f.result <- nil
	app.WaitUntil(time.Second, func() bool { return !app.Exists(remove) })
	if app.Exists("id:section:pinned") {
		t.Fatal("empty pinned section stayed visible")
	}
	if str(s.selected, "id") != "review" || s.draft.GetUntracked() != "keep this draft" {
		t.Fatal("removal changed another conversation")
	}
	if !s.removedSessions[sessionIdentity(Object{"deviceId": "local", "providerId": "pi", "id": "design"})] {
		t.Fatal("stale cache not fenced")
	}
}

func TestUnknownRemovalCannotBeResubmitted(t *testing.T) {
	f := &sessionRemovalFixture{newDemoAPI(), make(chan string, 4), make(chan error, 4)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		s.api = f
		s.target, s.selected = "local", s.sessions[2]
		s.draft.Set("removed session draft")
		return core.View{Style: fill, Children: []core.Node{s.directory(cx),
			button(cx, "partial-directory", "Partial directory", nil, "ghost", func() { s.applyEnvironmentPage("local", "pi", nil, false) }),
			button(cx, "present-directory", "Present directory", nil, "ghost", func() { s.applyEnvironmentPage("local", "pi", []Object{s.selected}, true) }),
			button(cx, "absent-directory", "Absent directory", nil, "ghost", func() { s.applyEnvironmentPage("local", "pi", nil, true) }),
			core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) })}}
	}, nsgotest.Size(800, 1000), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop}))
	ss := s.sessions[2]
	app.RightClick("id:" + sessionRowID("pinned", ss))
	app.Press("id:session-remove-action")
	app.Press("id:confirm-remove-session")
	app.WaitUntil(time.Second, func() bool { return len(f.calls) == 1 })
	f.result <- &control.ResponseError{Code: control.WriteOutcomeUnknown, Message: "ack lost"}
	app.WaitUntil(time.Second, func() bool {
		return app.Find("id:confirm-remove-session").Props.Disabled && !app.Find("id:confirm-remove-session").Props.Busy
	})
	app.Press("id:cancel-remove-session")
	app.RightClick("id:" + sessionRowID("pinned", ss))
	app.Press("id:session-remove-action")
	if !app.Find("id:confirm-remove-session").Props.Disabled || len(f.calls) != 1 {
		t.Fatal("unknown write was made retryable")
	}
	app.Press("id:cancel-remove-session")
	app.Press("id:partial-directory")
	app.Press("id:present-directory")
	if !s.uncertainRemovals[sessionIdentity(ss)] || s.selected == nil || s.draft.GetUntracked() != "removed session draft" {
		t.Fatal("partial or still-present directory settled an uncertain write")
	}
	app.Press("id:absent-directory")
	if s.uncertainRemovals[sessionIdentity(ss)] || s.pinned[sessionIdentity(ss)] != 0 || s.selected != nil || s.draft.GetUntracked() != "" || len(f.calls) != 1 {
		t.Fatal("confirmed absence did not clear the removed conversation without replay")
	}
}

func TestRemovalReconciliationClosesOnlyMatchingConversationAndDialog(t *testing.T) {
	for _, name := range []string{"removed", "other"} {
		t.Run(name, func(t *testing.T) {
			var s *State
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				s = sidebarInteractionFixture(cx)
				s.target, s.selected = "local", s.sessions[2]
				if name == "other" {
					s.target, s.selected = "remote", s.sessions[1]
				}
				s.draft.Set("draft")
				s.uncertainRemovals = map[string]bool{sessionIdentity(s.sessions[2]): true}
				s.requestSessionRemoval(s.selected)
				return button(cx, "reconcile-removal", "Reconcile", nil, "ghost", func() { s.applyEnvironmentPage("local", "pi", nil, true) })
			})
			app.Press("id:reconcile-removal")
			if name == "other" {
				if str(s.selected, "id") != "review" || s.draft.GetUntracked() != "draft" || s.dialog.GetUntracked() != "remove-session" {
					t.Fatal("reconciliation changed another conversation or dialog")
				}
			} else if s.selected != nil || s.draft.GetUntracked() != "" || s.dialog.GetUntracked() == "remove-session" {
				t.Fatal("reconciliation kept the removed conversation or confirmation open")
			}
		})
	}
}

func TestSessionTooltipAndContextualActions(t *testing.T) {
	if nsgotest.Target() == "macos" {
		t.Skip("native automation also fails the unchanged message-navigation hover control; covered headlessly and in browser")
	}
	var state *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := sidebarInteractionFixture(cx)
		state = s
		item := directoryRow{ID: "fixture", Title: str(s.sessions[0], "title"), Path: str(s.sessions[0], "cwd"), Session: s.sessions[0], Section: "all"}
		return core.View{Style: fill, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(390), Margin: core.Edges{Top: 80}}, Children: []core.Node{s.sessionRow(cx, func() directoryRow { return item }, &listTouchBrake{})}},
		}}
	}, nsgotest.Size(800, 420), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop, OS: core.OSMacOS}))
	app.SetEnvironment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop, OS: core.OSMacOS})
	row := sessionSelector("local", "codex-app", "build")
	app.Hover(row)
	app.Advance(500 * time.Millisecond)
	app.WaitFor("id:session-hover-card", time.Second)
	card := app.Find("id:session-hover-card")
	b := globalBounds(card)
	if b.Size.Width < 100 || b.Size.Width > 340 || b.Size.Height < 50 || card.Parent.Style.BorderWidth == 0 {
		t.Fatal("invalid tooltip surface", b)
	}
	app.Screenshot("session-hover-card-and-actions")
	app.HoverOut()
	app.Key("tab")
	app.Focus(row)
	app.Key("tab")
	app.Key("enter")
	if state.pinned[sessionIdentity(state.sessions[0])] == 0 {
		t.Fatal("keyboard did not activate pin action")
	}
	app.Screenshot("session-keyboard-actions")
}
