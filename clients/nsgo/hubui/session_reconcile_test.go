package hubui

import (
	"fmt"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

func TestCachedCursorStillReconcilesWithoutLiveJoin(t *testing.T) {
	f := newFailedSubscriptionAPI(true)
	cached := lifecycleSyncTranscript("inProgress", "旧进度", 7)
	cached["resume"] = Object{"epoch": "live", "cursor": 7}
	app, s := conversationFixture(t, f, 390, cached, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	// The persisted cursor only proves the cached prefix is internally
	// consistent. A blocked watch must not prevent an independent fresh read.
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	(<-f.snapshots).reply <- lifecycleSyncTranscript("completed", "离线期间完成的回复", 7)
	app.WaitFor("text:离线期间完成的回复", time.Second)
	app.WaitUntil(time.Second, func() bool { return activeTurn(s.transcript) == nil && !s.stream.snapshotInFlight })
}

func TestNotificationReconciliationRecoversWithoutPolling(t *testing.T) {
	for _, target := range []string{"local", "demo-device"} {
		for _, hidden := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/hidden=%t", target, hidden), func(t *testing.T) {
				app, s, f := lifecycleSyncFixture(t, target, core.OSMacOS)
				input, stream := app.Find("id:chat-input").ID, s.stream
				app.Post(func() {
					s.cx.Post(func() {
						s.setBackground(hidden)
					})
				})
				app.Settle()
				// A heartbeat and normal application ticks must not download history.
				f.events <- Object{"epoch": "live", "cursor": 1, "heartbeat": true}
				app.WaitUntil(time.Second, func() bool { return s.stream.cursor == 1 })
				app.Post(func() { s.cx.Post(s.poll) })
				app.Settle()
				if f.reads.Load() != 1 {
					t.Fatal("healthy heartbeat polled history")
				}
				f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "snapshot"}}}
				app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
				request := <-f.snapshots
				// A slow read must not accumulate another read on each tick, and
				// live output must remain independent while it is pending.
				app.Post(func() { s.cx.Post(func() { s.poll(); s.poll(); s.poll() }) })
				f.events <- Object{"epoch": "live", "cursor": 2, "events": []Object{{"id": 2, "kind": "text", "turnId": "work", "itemId": "answer", "offset": 3, "delta": "仍可流式更新"}}}
				app.WaitUntil(time.Second, func() bool { return s.stream.cursor == 2 })
				if s.stream.snapshotAgain || f.reads.Load() != 2 || app.Exists("id:transcript-syncing") {
					t.Fatal("notification-driven checks queued duplicate reads or disrupted live presentation")
				}
				request.reply <- lifecycleSyncTranscript("completed", "无需切换即可补齐最终回复", 2)
				app.WaitFor("text:无需切换即可补齐最终回复", time.Second)
				app.WaitUntil(time.Second, func() bool { return activeTurn(s.transcript) == nil && !s.stream.snapshotInFlight })
				app.Post(func() { s.cx.Post(func() { s.poll(); s.poll() }) })
				app.Settle()
				if s.stream != stream || f.watchCancelled.Load() != 0 || f.reads.Load() != 2 || f.writes.Load() != 0 || app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "保留当前草稿" {
					t.Fatal("reconciliation replaced a watch/editor, modified the draft, repeated reads or sent a command")
				}
				// A stream reset can also reveal a new turn after an idle period.
				f.events <- Object{"epoch": "live", "cursor": 3, "events": []Object{{"id": 3, "kind": "snapshot"}}}
				app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
				next := lifecycleSyncTranscript("inProgress", "补齐外部开始的新一轮", 3)
				objects(next["turns"])[0]["id"] = "next-work"
				(<-f.snapshots).reply <- next
				app.WaitFor("text:补齐外部开始的新一轮", time.Second)
				app.WaitUntil(time.Second, func() bool { return str(activeTurn(s.transcript), "id") == "next-work" })
				app.Screenshot(fmt.Sprintf("notification-sync-restored-%s-hidden-%t", target, hidden))
			})
		}
	}
}

func TestFailedWatchReconcilesAndRespectsHistoryBackoff(t *testing.T) {
	f := newFailedSubscriptionAPI(false)
	app, s := conversationFixture(t, f, 390, nil, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 && len(f.watchEpochs) > 0 })
	(<-f.snapshots).reply <- lifecycleSyncTranscript("inProgress", "旧进度", 0)
	app.WaitUntil(time.Second, func() bool { return s.stream.baseline && !s.stream.snapshotInFlight })
	f.failHistory.Store(true)
	app.Post(func() {
		s.cx.Post(func() {
			s.startSessionWatch()
		})
	})
	app.WaitUntil(time.Second, func() bool { return s.stream.historyError != nil })
	app.Post(func() { s.cx.Post(func() { s.poll(); s.poll(); s.poll() }) })
	app.Settle()
	if len(f.snapshots) != 0 || s.stream.snapshotInFlight || activeTurn(s.transcript) == nil {
		t.Fatal("notification-driven check bypassed history backoff or erased the last usable state")
	}
	// Expire the isolated retry delay, without waiting for or replacing watch.
	app.Post(func() { s.cx.Post(func() { s.stream.nextSnapshot = time.Time{}; s.stream.startSnapshot() }) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	(<-f.snapshots).reply <- lifecycleSyncTranscript("completed", "订阅失败也能恢复最终回复", 0)
	app.WaitFor("text:订阅失败也能恢复最终回复", time.Second)
	app.WaitUntil(time.Second, func() bool { return activeTurn(s.transcript) == nil && s.stream.historyError == nil })
}

func TestNotificationReconciliationCancelsOnNavigation(t *testing.T) {
	app, s, f := lifecycleSyncFixture(t, "local", core.OSMacOS)
	stream := s.stream
	f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "snapshot"}}}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	request := <-f.snapshots
	app.Post(func() { s.cx.Post(s.showHome) })
	app.WaitUntil(time.Second, func() bool { return stream.ctx.Err() != nil })
	request.reply <- lifecycleSyncTranscript("completed", "离开后不应显示", 0)
	app.Post(func() { s.cx.Post(s.poll) })
	app.Settle()
	if stream.current() || app.Exists("text:离开后不应显示") || f.reads.Load() != 2 {
		t.Fatal("cancelled reconciliation updated another view or restarted")
	}
}
