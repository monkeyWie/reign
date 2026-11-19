package hubui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type focusDraftAPI struct {
	API
	release <-chan struct{}
}

func (a *focusDraftAPI) Call(ctx context.Context, method string, params Object, target string, request Object) (any, error) {
	if method == "client.draft.get" {
		if str(params, "session") == "design" {
			select {
			case <-a.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return Object{"text": "restored"}, nil
	}
	return a.API.Call(ctx, method, params, target, request)
}

func composerFocusFixture(t *testing.T, release <-chan struct{}) (*nsgotest.App, *State) {
	t.Helper()
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		if release != nil {
			s.api = &focusDraftAPI{s.api, release}
			s.draftWriter = newDraftWriter(s.ctx, s.api, func(error) {})
			s.draftLoads = map[string]string{}
			s.loadDraft()
		}
		return core.View{Style: fill, Children: []core.Node{s.workspaceView(cx), core.Input{TestID: "other-input", Label: "Other editor", Style: core.Style{Height: core.Pt(36), FlexShrink: 0}}}}
	}, nsgotest.Size(1100, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	return app, s
}

func TestComposerFocusOnEntryWithoutStealingUpdates(t *testing.T) {
	app, s := composerFocusFixture(t, nil)
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Focused })
	// Key sends to the actual keyboard target; Type(selector) would first focus
	// the field and hide a broken autofocus on native/Web targets.
	app.Key("a")
	if app.Find("id:chat-input").Text != "a" {
		t.Fatal("entering the conversation did not accept typing")
	}
	editor := app.Find("id:chat-input").ID
	app.Focus("id:other-input")
	app.Post(func() { s.setAppearance("dark"); s.changed() })
	app.Settle()
	if !app.Find("id:other-input").Focused || app.Find("id:chat-input").ID != editor {
		t.Fatal("background/theme update stole focus or replaced the editor")
	}
	app.Press("id:" + sessionRowID("all", s.sessions[1]))
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Focused })
	app.Key("b")
	if app.Find("id:chat-input").Text != "b" {
		t.Fatal("switching to the remote conversation did not accept typing")
	}
	app.Press("id:" + sessionRowID("pinned", s.sessions[2]))
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Focused })
	if app.Find("id:chat-input").Text != "a" {
		t.Fatal("returning to the conversation lost its draft")
	}
}

func TestComposerFocusAfterDraftRestore(t *testing.T) {
	release := make(chan struct{})
	app, _ := composerFocusFixture(t, release)
	if field := app.Find("id:chat-input"); !field.Props.Disabled || field.Focused {
		t.Fatal("unrestored draft accepted focus")
	}
	close(release)
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Focused })
	if field := app.Find("id:chat-input"); field.Props.Disabled || field.Text != "restored" {
		t.Fatal("focus arrived before the restored draft")
	}
	app.Key("x")
	if text := app.Find("id:chat-input").Text; len(text) != len("restoredx") || strings.ReplaceAll(text, "x", "") != "restored" {
		t.Fatalf("restored editor did not receive actual keyboard input: %q", text)
	}
}

func TestComposerLateDraftDoesNotStealFocusAfterNavigation(t *testing.T) {
	release := make(chan struct{})
	app, s := composerFocusFixture(t, release)
	app.Press("id:" + sessionRowID("all", s.sessions[1]))
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Focused })
	app.Focus("id:other-input")
	close(release)
	app.WaitUntil(time.Second, func() bool { return s.draftLoads["local|pi|design"] == "ready" })
	app.Settle()
	if !app.Find("id:other-input").Focused {
		t.Fatal("late restoration from the old session stole keyboard focus")
	}
}
