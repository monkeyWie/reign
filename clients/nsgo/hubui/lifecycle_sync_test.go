package hubui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type lifecycleSyncAPI struct {
	*independentStreamAPI
	watchCancelled, approvalsCancelled atomic.Int32
	watchStarted                       atomic.Bool
	writes, catalogues, presence       atomic.Int32
	saved                              chan Object
}

func (f *lifecycleSyncAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.list" {
		f.presence.Add(1)
		return []Object{{"id": "local", "local": true, "connected": true}, {"id": "demo-device", "connected": true}}, nil
	}
	// Both routes expose the same isolated session, including catalogue reads
	// triggered by the native window becoming visible during test startup.
	if target == "local" {
		target = "demo-device"
	}
	if method == "client.snapshot" {
		select {
		case f.saved <- object(p["transcript"]):
		default:
		}
		return nil, nil
	}
	if method == "client.sync" {
		f.catalogues.Add(1)
	}
	if method == "client.call" && (str(r, "method") == "send" || str(r, "method") == "steer") {
		f.writes.Add(1)
	}
	if method == "client.call" && str(r, "method") == "watch" && !flag(r, "probe") {
		f.watchStarted.Store(true)
	}
	v, err := f.independentStreamAPI.Call(ctx, method, p, target, r)
	if method == "client.call" && str(r, "method") == "watch" && ctx.Err() != nil {
		f.watchCancelled.Add(1)
	}
	if method == "client.call" && str(r, "method") == "approvals" && ctx.Err() != nil {
		f.approvalsCancelled.Add(1)
	}
	return v, err
}

func lifecycleSyncFixture(t *testing.T, target string, platform core.OS) (*nsgotest.App, *State, *lifecycleSyncAPI) {
	t.Helper()
	f := &lifecycleSyncAPI{independentStreamAPI: &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}, saved: make(chan Object, 16)}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.width.Set(1100)
		s.devices = []Object{{"id": target, "connected": true}}
		s.target = target
		s.selected = Object{"id": "demo-chat", "providerId": "codex-app", "title": "隔离同步测试"}
		s.draft.Set("保留当前草稿")
		s.observeLifecycle()
		return s.conversation(cx, true)
	}, nsgotest.Size(1100, 844), nsgotest.Environment(core.Environment{OS: platform, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Post(func() {
		s.cx.Post(func() {
			if s.stream == nil || !s.stream.current() {
				s.startSessionWatch()
			}
		})
	})
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 && s.stream.epoch == "live" })
	request := <-f.snapshots
	request.reply <- lifecycleSyncTranscript("inProgress", "旧进度", 0)
	app.WaitFor("text:旧进度", time.Second)
	app.WaitUntil(time.Second, func() bool { return s.stream.baseline && !s.stream.snapshotInFlight })
	return app, s, f
}

func lifecycleSyncTranscript(status, text string, cursor int64) Object {
	return Object{"streamEpoch": "live", "streamCursor": cursor, "session": Object{"id": "demo-chat"}, "turns": []Object{{"id": "work", "status": status, "items": []Object{{"id": "answer", "role": "assistant", "phase": "final_answer", "text": text}}}}}
}

func TestBackgroundConversationReceivesAndPersistsCompletion(t *testing.T) {
	for _, platform := range []core.OS{core.OSMacOS, core.OSWindows, core.OSLinux, core.OSAndroid, core.OSIOS, core.OSWeb} {
		for _, target := range []string{"local", "demo-device"} {
			t.Run(string(platform)+"/"+target, func(t *testing.T) {
				app, s, f := lifecycleSyncFixture(t, target, platform)
				input, stream := app.Find("id:chat-input").ID, s.stream
				// Same callback used for app/tab backgrounding and window hiding.
				app.Post(func() { s.cx.Post(func() { s.setBackground(true) }) })
				app.Settle()
				// Directory/presence reads remain available while the view is hidden.
				catalogues, presence := f.catalogues.Load(), f.presence.Load()
				app.Post(func() { s.cx.Post(s.refreshSessions) })
				app.WaitUntil(time.Second, func() bool { return f.catalogues.Load() > catalogues && f.presence.Load() > presence })
				f.events <- Object{"epoch": "live", "cursor": 2, "events": []Object{
					{"id": 1, "kind": "item", "turnId": "work", "itemId": "answer", "item": Object{"id": "answer", "role": "assistant", "phase": "final_answer", "text": "最终回复已完成"}},
					{"id": 2, "kind": "turn", "turnId": "work", "status": "completed"},
				}}
				app.WaitFor("text:最终回复已完成", time.Second)
				app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
				(<-f.snapshots).reply <- lifecycleSyncTranscript("completed", "最终回复已完成", 2)
				app.WaitUntil(time.Second, func() bool { return !s.stream.snapshotInFlight && activeTurn(s.transcript) == nil })
				app.WaitUntil(2*time.Second, func() bool {
					for len(f.saved) > 0 {
						if snapshot := <-f.saved; activeTurn(snapshot) == nil && len(objects(snapshot["turns"])) > 0 && number(object(snapshot["resume"]), "cursor") == 2 {
							return true
						}
					}
					return false
				})
				if !s.backgrounded || s.stream != stream || f.watchCancelled.Load() != 0 || f.approvalsCancelled.Load() != 0 || f.writes.Load() != 0 || app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "保留当前草稿" {
					t.Fatal("hiding interrupted synchronization or changed the draft")
				}
			})
		}
	}
}

func TestBackgroundSynchronizationStopsOnApplicationCancellation(t *testing.T) {
	app, s, f := lifecycleSyncFixture(t, "demo-device", core.OSIOS)
	app.WaitUntil(time.Second, func() bool { return f.approvalOnce.Load() && f.watchStarted.Load() })
	app.Post(func() { s.cx.Post(func() { s.setBackground(true); s.cancel() }) })
	app.WaitUntil(time.Second, func() bool { return f.watchCancelled.Load() > 0 && f.approvalsCancelled.Load() > 0 })
	before := f.reads.Load()
	app.Post(func() { s.cx.Post(func() { s.setBackground(false); s.poll(); s.startSessionWatch() }) })
	app.Settle()
	if f.reads.Load() != before || s.stream.current() {
		t.Fatal("visibility restarted work after application cancellation")
	}
}

func TestForegroundReconcilesSilentCompletionWithoutReplacingWatch(t *testing.T) {
	for _, platform := range []core.OS{core.OSMacOS, core.OSWindows, core.OSLinux, core.OSAndroid, core.OSIOS, core.OSWeb} {
		t.Run(string(platform), func(t *testing.T) {
			app, s, f := lifecycleSyncFixture(t, "demo-device", platform)
			input, stream := app.Find("id:chat-input").ID, s.stream
			app.Post(func() { s.cx.Post(func() { s.setBackground(true); s.setBackground(false) }) })
			// A valid cursor and a healthy watch can coexist with a missed upstream
			// completion. Returning to the window must independently reconcile history.
			app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
			(<-f.snapshots).reply <- lifecycleSyncTranscript("completed", "恢复时补齐最终回复", 0)
			app.WaitFor("text:恢复时补齐最终回复", time.Second)
			app.WaitUntil(time.Second, func() bool { return activeTurn(s.transcript) == nil && !s.stream.snapshotInFlight })
			if s.stream != stream || f.watchCancelled.Load() != 0 || f.reads.Load() != 2 || f.writes.Load() != 0 || app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "保留当前草稿" {
				t.Fatal("foreground reconciliation replaced a healthy watch, repeated reads or modified a draft")
			}
			app.Screenshot("foreground-sync-restored-" + string(platform))
		})
	}
}
