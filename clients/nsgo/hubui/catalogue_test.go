package hubui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type lazyCatalogueAPI struct {
	*demoAPI
	release                    chan struct{}
	syncs, transcripts, models atomic.Int32
}

type refreshCatalogueAPI struct {
	*lazyCatalogueAPI
	fail bool
}

func (f *refreshCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	v, err := f.lazyCatalogueAPI.Call(ctx, method, p, target, r)
	if err == nil && f.fail && method == "client.sync" && str(p, "provider") == "codex-app" {
		return nil, errors.New("fixture catalogue unavailable")
	}
	return v, err
}

func TestSessionRefreshFeedback(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			f := &refreshCatalogueAPI{lazyCatalogueAPI: &lazyCatalogueAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}, fail: outcome == "failure"}
			var s *State
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetTheme(core.DefaultTheme())
				s = newState(cx, f)
				s.devices = []Object{{"id": "demo-device", "connected": true}}
				s.target = "demo-device"
				s.sessions = []Object{{"id": "demo-chat", "providerId": "codex-app", "title": "Cached conversation", "cwd": "/workspace/agenthub"}}
				return s.directory(cx)
			}, nsgotest.Size(390, 844))
			app.Press("id:refresh-sessions")
			app.WaitFor("id:refresh-sessions-loading", time.Second)
			app.WaitUntil(time.Second, func() bool { return f.syncs.Load() == 1 })
			control := app.Find("id:refresh-sessions")
			if !control.Props.Busy || !control.Props.Disabled || control.Props.Label == "" {
				t.Fatal("refresh must acknowledge pending work and prevent duplicate activation")
			}
			spinner := app.Find("id:refresh-sessions-loading").ID
			for range 3 {
				app.PressAt("id:refresh-sessions", core.Point{X: 12, Y: 12})
				app.Post(func() { s.cx.Post(s.changed) })
				app.Settle()
				if app.Find("id:refresh-sessions-loading").ID != spinner {
					t.Fatal("unrelated updates restarted refresh animation")
				}
			}
			if f.syncs.Load() != 1 {
				t.Fatal("duplicate refresh request")
			}
			app.WaitFor("id:session-demo-chat", time.Second)
			app.Screenshot("session-refresh-" + outcome)
			if outcome == "cancel" {
				app.Post(func() { s.cx.Post(func() { s.cancelCatalogue(); s.changed() }) })
			} else {
				close(f.release)
			}
			app.WaitUntil(time.Second, func() bool { return !app.Exists("id:refresh-sessions-loading") })
			control = app.Find("id:refresh-sessions")
			if control.Props.Busy || control.Props.Disabled {
				t.Fatal("refresh feedback did not finish")
			}
			app.WaitFor("id:session-demo-chat", time.Second)
		})
	}
}

type catalogueReply struct {
	ctx    context.Context
	result chan []Object
}

type orderedCatalogueAPI struct {
	*demoAPI
	requests chan catalogueReply
}

func (*orderedCatalogueAPI) CanHost() bool { return false }
func (f *orderedCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.sync" && str(p, "provider") == "codex-app" {
		request := catalogueReply{ctx, make(chan []Object, 1)}
		f.requests <- request
		// Model a response already arriving when cancellation races completion.
		return <-request.result, nil
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestSessionRefreshBackgroundIsSilentAndManualSupersedesIt(t *testing.T) {
	f := &orderedCatalogueAPI{newDemoAPI(), make(chan catalogueReply, 4)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.devices = []Object{{"id": "demo-device", "connected": true}}
		s.target = "demo-device"
		s.sessions = []Object{{"id": "cached", "providerId": "codex-app", "title": "Cached conversation", "cwd": "/workspace/agenthub"}}
		return s.directory(cx)
	}, nsgotest.Size(390, 844))
	next := func() catalogueReply {
		t.Helper()
		app.WaitUntil(2*time.Second, func() bool { return len(f.requests) > 0 })
		select {
		case r := <-f.requests:
			t.Cleanup(func() {
				select {
				case r.result <- nil:
				default:
				}
			})
			return r
		case <-time.After(2 * time.Second):
			t.Fatal("catalogue request never started")
			return catalogueReply{}
		}
	}
	app.Post(func() { s.cx.Post(s.poll) })
	background := next()
	app.Settle()
	control := app.Find("id:refresh-sessions")
	if control.Props.Busy || control.Props.Disabled || app.Exists("id:refresh-sessions-loading") {
		t.Fatal("background refresh animated or disabled the manual refresh control")
	}
	app.WaitFor("id:session-cached", time.Second)
	app.Screenshot("background-refresh-silent")
	app.PressAt("id:refresh-sessions", core.Point{X: 12, Y: 12})
	manual := next()
	app.WaitFor("id:refresh-sessions-loading", time.Second)
	select {
	case <-background.ctx.Done():
	default:
		t.Fatal("manual refresh did not cancel the older read")
	}
	background.result <- []Object{{"id": "stale", "providerId": "codex-app", "title": "Stale conversation"}}
	app.Advance(100 * time.Millisecond)
	if !app.Exists("id:refresh-sessions-loading") || app.Exists("id:session-stale") {
		t.Fatal("stale background response affected the manual request")
	}
	manual.result <- []Object{{"id": "fresh", "providerId": "codex-app", "title": "Fresh conversation"}}
	app.WaitFor("id:session-fresh", time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:refresh-sessions-loading") })
	if app.Exists("id:session-stale") || app.Find("id:refresh-sessions").Props.Disabled {
		t.Fatal("manual refresh did not finish with the fresh result")
	}
}

func (*lazyCatalogueAPI) CanHost() bool { return false }
func (f *lazyCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.cached" {
		return nil, nil
	}
	if method == "client.sync" && str(p, "provider") == "codex-app" {
		f.syncs.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
		list, err := f.demoAPI.Call(ctx, method, p, target, r)
		return append(objects(list), Object{"id": "older-chat", "providerId": "codex-app", "title": "更早的会话", "cwd": "/workspace/agenthub"}), err
	}
	if method == "client.call" {
		switch str(r, "method") {
		case "transcript":
			f.transcripts.Add(1)
		case "models":
			f.models.Add(1)
		case "watch":
			if flag(r, "probe") {
				return Object{"epoch": "catalogue", "cursor": number(r, "after"), "reset": str(r, "epoch") != "catalogue"}, nil
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func TestColdCatalogueIsUsableBeforeFullScanAndMessagesLoadOnOpen(t *testing.T) {
	f := &lazyCatalogueAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844))
	app.WaitFor("id:device-demo-device", time.Second)
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-demo-chat", time.Second)
	// A different provider must remain usable while the App directory is blocked.
	app.WaitFor("id:session-demo-cli", time.Second)
	app.WaitUntil(time.Second, func() bool { return f.syncs.Load() > 0 })
	if f.transcripts.Load() != 0 || f.models.Load() != 0 {
		t.Fatal("opening a device eagerly loaded messages/models")
	}
	app.Press("id:session-demo-chat")
	app.WaitUntil(time.Second, func() bool { return f.transcripts.Load() == 1 })
	app.WaitFor("id:message-demo-turn|demo-assistant", time.Second)
	close(f.release)
	app.Press("id:back-sessions")
	app.WaitFor("id:session-older-chat", time.Second)
	if f.transcripts.Load() != 1 {
		t.Fatal("directory completion loaded unrelated transcripts")
	}
}
