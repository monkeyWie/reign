package hubui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
)

type failedSubscriptionAPI struct {
	*independentStreamAPI
	blocked     bool
	failHistory atomic.Bool
	watchEpochs chan string
}

func (f *failedSubscriptionAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" {
		switch str(r, "method") {
		case "watch":
			select {
			case f.watchEpochs <- str(r, "epoch"):
			default:
			}
			if f.blocked {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return nil, errors.New("暂不支持测试 IPC 状态版本")
		case "transcript":
			if f.failHistory.Swap(false) {
				return nil, errors.New("测试历史结构不支持")
			}
		}
	}
	return f.independentStreamAPI.Call(ctx, method, p, target, r)
}
func newFailedSubscriptionAPI(blocked bool) *failedSubscriptionAPI {
	return &failedSubscriptionAPI{independentStreamAPI: &independentStreamAPI{
		demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{}),
	}, blocked: blocked, watchEpochs: make(chan string, 4)}
}
func TestInitialHistoryDoesNotWaitForFailedOrBlockedSubscription(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "failed"
		if blocked {
			name = "blocked"
		}
		t.Run(name, func(t *testing.T) {
			f := newFailedSubscriptionAPI(blocked)
			app, s := conversationFixture(t, f, 390, nil, true)
			app.Post(func() {
				s.cx.Post(func() {
					// A cursor without its persisted messages cannot establish a baseline.
					s.streamKey, s.streamEpoch, s.streamCursor = s.key(), "missing-baseline", 100
					s.startSessionWatch()
				})
			})
			app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 && len(f.watchEpochs) > 0 })
			if epoch := <-f.watchEpochs; epoch != "" {
				t.Fatalf("resumed cursor without content: %s", epoch)
			}
			request := <-f.snapshots
			app.WaitFor("id:transcript-loading", time.Second)
			if app.Exists("id:transcript-syncing") {
				t.Fatal("initial loading duplicated catch-up dots")
			}
			request.fragments <- provider.Transcript{StreamEpoch: "history", Session: provider.Session{ID: "demo-chat"}, Turns: []provider.Turn{{ID: "t", Status: "completed", Items: []provider.Item{{ID: "a", Role: "assistant", Text: "独立历史第一条"}}}}}
			app.WaitFor("text:独立历史第一条", time.Second)
			app.WaitFor("id:transcript-syncing", time.Second)
			request.reply <- Object{"streamEpoch": "history", "streamCursor": 3, "turns": []Object{{"id": "t", "status": "completed", "items": []Object{{"id": "a", "role": "assistant", "text": "独立历史第一条"}}}}}
			app.WaitUntil(time.Second, func() bool { return !app.Exists("id:transcript-syncing") })
			app.Find("text:独立历史第一条")
			if f.reads.Load() != 1 {
				t.Fatalf("repeated initial history without live join: %d", f.reads.Load())
			}
		})
	}
}
func TestFailedHistoryShowsRetryWhileDeviceRemainsConnected(t *testing.T) {
	f := newFailedSubscriptionAPI(false)
	f.failHistory.Store(true)
	app, s := conversationFixture(t, f, 390, nil, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitFor("id:retry-transcript", time.Second)
	app.Find("text:消息加载失败：测试历史结构不支持")
	if app.Exists("text:此会话尚无本地消息，连接后会自动加载。") || app.Exists("id:transcript-loading") {
		t.Fatal("history failure presented as offline or endless loading")
	}
	app.Press("id:retry-transcript")
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	request := <-f.snapshots
	request.reply <- Object{"streamEpoch": "history", "streamCursor": 1, "turns": []Object{{"id": "t", "status": "completed", "items": []Object{{"id": "a", "role": "assistant", "text": "重试已恢复"}}}}}
	app.WaitFor("text:重试已恢复", time.Second)
	if app.Exists("id:retry-transcript") {
		t.Fatal("successful history retained retry placeholder")
	}
	app.Screenshot("history-retry-restored")
}
func TestInitialHistoryRejectsReplacedStreamBoundary(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	app, s := conversationFixture(t, f, 390, nil, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 && s.stream.epoch == "live" })
	initial := <-f.snapshots
	f.events <- Object{"epoch": "replacement", "cursor": 0, "reset": true}
	app.WaitUntil(time.Second, func() bool { return s.stream.epoch == "replacement" })
	initial.reply <- Object{"streamEpoch": "live", "turns": []Object{{"id": "old", "status": "completed", "items": []Object{{"id": "a", "role": "assistant", "text": "旧流消息"}}}}}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	replacement := <-f.snapshots
	if app.Exists("text:旧流消息") {
		t.Fatal("superseded history committed")
	}
	replacement.reply <- Object{"streamEpoch": "replacement", "turns": []Object{{"id": "new", "status": "completed", "items": []Object{{"id": "b", "role": "assistant", "text": "新流消息"}}}}}
	app.WaitFor("text:新流消息", time.Second)
	if f.reads.Load() != 2 {
		t.Fatalf("replacement history looped: %d", f.reads.Load())
	}
}

func TestInitialHistoryBeforeLiveJoinCannotSkipTheOwnersSnapshot(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	app, s := conversationFixture(t, f, 390, nil, true)
	// Model an owner publishing its current state after the independent disk
	// history starts, before the live join returns its reset boundary.
	app.Post(func() {
		s.cx.Post(func() {
			ctx, cancel := context.WithCancel(s.ctx)
			s.watchCancel = cancel
			w := &sessionStream{s: s, ctx: ctx, target: s.target, provider: "codex-app", session: "demo-chat", key: s.key(), approvals: make(chan struct{}, 1)}
			s.stream = w
			w.startSnapshot()
			w.batch(Object{"epoch": "live", "cursor": 10, "reset": true})
		})
	})
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	initial := <-f.snapshots
	initial.reply <- Object{"streamEpoch": "live", "streamCursor": 0, "turns": []Object{{"id": "t", "status": "completed", "items": []Object{{"id": "a", "role": "assistant", "text": "旧的持久化回复"}}}}}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	fresh := <-f.snapshots
	if app.Exists("text:旧的持久化回复") {
		t.Fatal("pre-join history skipped owner state")
	}
	fresh.reply <- Object{"streamEpoch": "live", "streamCursor": 10, "turns": []Object{{"id": "t", "status": "inProgress", "items": []Object{{"id": "a", "role": "assistant", "text": "App 当前最新回复"}}}}}
	app.WaitFor("text:App 当前最新回复", time.Second)
	if f.reads.Load() != 2 {
		t.Fatalf("join boundary failed to converge: %d", f.reads.Load())
	}
}
