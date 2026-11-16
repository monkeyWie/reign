package hubui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type snapshotRequest struct {
	reply     chan Object
	fragments chan provider.Transcript
}
type independentStreamAPI struct {
	*demoAPI
	snapshots        chan snapshotRequest
	events           chan Object
	approvalsStarted chan struct{}
	approvalOnce     atomic.Bool
	reads            atomic.Int32
}

func (f *independentStreamAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" {
		switch str(r, "method") {
		case "transcript":
			f.reads.Add(1)
			request := snapshotRequest{reply: make(chan Object, 1), fragments: make(chan provider.Transcript, 8)}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case f.snapshots <- request:
			}
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case fragment := <-request.fragments:
					if err := provider.ObserveTranscript(ctx, fragment); err != nil {
						return nil, err
					}
				case t := <-request.reply:
					return t, nil
				}
			}
		case "approvals":
			if f.approvalOnce.CompareAndSwap(false, true) {
				close(f.approvalsStarted)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		case "watch":
			if str(r, "epoch") == "" {
				return Object{"epoch": "live", "cursor": 0, "reset": true}, nil
			}
			if flag(r, "probe") {
				return Object{"epoch": "live", "cursor": number(r, "after")}, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case b := <-f.events:
				return b, nil
			}
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func TestLiveTextDoesNotWaitForSnapshotsOrApprovals(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	cached := Object{"turns": []Object{{"id": "cached", "status": "completed", "items": []Object{{"id": "old", "role": "assistant", "text": "本地已有消息"}}}}}
	app, s := conversationFixture(t, f, 390, cached, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	var request snapshotRequest
	select {
	case request = <-f.snapshots:
	case <-time.After(time.Second):
		t.Fatal("snapshot never started")
	}
	select {
	case <-f.approvalsStarted:
	case <-time.After(time.Second):
		t.Fatal("approval worker never started")
	}
	app.WaitFor("id:transcript-syncing", time.Second)
	app.Find("text:本地已有消息")
	app.Screenshot("message-catch-up-dots")
	f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "text", "turnId": "reply", "itemId": "answer", "offset": 0, "delta": "第一段🙂"}}}
	app.WaitFor("text:第一段🙂", time.Second)
	// This stale response must replay the text that arrived while it waited.
	request.reply <- Object{"streamEpoch": "live", "streamCursor": 0, "turns": []Object{{"id": "reply", "status": "inProgress", "items": []Object{}}}}
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:transcript-syncing") })
	app.Find("text:第一段🙂")
	f.events <- Object{"epoch": "live", "cursor": 2, "events": []Object{{"id": 2, "kind": "text", "turnId": "reply", "itemId": "answer", "offset": 5, "delta": "第二段"}}}
	waitAnimatedText(app, "第一段🙂第二段", time.Second)
	f.events <- Object{"epoch": "live", "cursor": 2, "heartbeat": true}
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.streamCursor == 2 })
	// A caught-up probe on reconnect must not download history or show dots.
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.Settle()
	if f.reads.Load() != 1 {
		t.Fatalf("repeated history reads blocked the stream: %d", f.reads.Load())
	}
	if app.Exists("id:transcript-syncing") {
		t.Fatal("caught-up session shows synchronization")
	}
	// Successful sends on the joined stream must keep using event deltas;
	// requesting the recent history again would compete with every small reply.
	var checked atomic.Bool
	app.Post(func() {
		s.cx.Post(func() {
			s.refreshAfterSend(s.key(), s.target, s.stream.provider, s.stream.session)
			if s.stream.snapshotInFlight || s.stream.snapshotAgain {
				t.Error("successful send restarted a joined history snapshot")
			}
			checked.Store(true)
		})
	})
	app.WaitUntil(time.Second, checked.Load)
	app.Screenshot("sync-complete-live-reply")
}

func TestFirstHistoryDisplaysMessagesBeforeTheFinalResponse(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	app, s := conversationFixture(t, f, 390, Object{}, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	var request snapshotRequest
	select {
	case request = <-f.snapshots:
	case <-time.After(time.Second):
		t.Fatal("history did not start")
	}
	fragment := provider.Transcript{StreamEpoch: "live", Session: provider.Session{ID: "chat"}, Turns: []provider.Turn{{ID: "history", Status: "completed", Items: []provider.Item{{ID: "one", Role: "user", Text: "历史第一条"}}}}}
	request.fragments <- fragment
	app.WaitFor("text:历史第一条", time.Second)
	app.WaitFor("id:transcript-syncing", time.Second)
	// A second message in a completed turn is additive until the final response.
	fragment.Turns = []provider.Turn{{ID: "history", Status: "completed", Items: []provider.Item{{ID: "two", Role: "assistant", Text: "历史第二条"}}}}
	request.fragments <- fragment
	app.WaitFor("text:历史第二条", time.Second)
	app.Find("text:历史第一条")
	f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "text", "turnId": "live-reply", "itemId": "answer", "offset": 0, "delta": "同步期间的实时回复"}}}
	app.WaitFor("text:同步期间的实时回复", time.Second)
	// The final history can be older than the live text already rendered.
	request.reply <- Object{"streamEpoch": "live", "streamCursor": 0, "turns": []Object{{"id": "history", "status": "completed", "items": []Object{{"id": "one", "role": "user", "text": "历史第一条"}, {"id": "two", "role": "assistant", "text": "历史第二条"}}}}}
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:transcript-syncing") })
	app.Find("text:历史第一条")
	app.Find("text:历史第二条")
	app.Find("text:同步期间的实时回复")
	app.Screenshot("progressive-first-history-complete")
}

func TestCaughtUpRunningAgentDoesNotShowSyncDotsDuringTailRefresh(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	live := Object{"streamEpoch": "live", "streamCursor": 0, "turns": []Object{{"id": "reply", "status": "inProgress", "items": []Object{{"id": "answer", "role": "assistant", "text": "正在回复"}}}}}
	app, s := conversationFixture(t, f, 390, live, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	first := <-f.snapshots
	// A cached in-progress turn alone does not prove history is caught up.
	app.WaitFor("id:transcript-syncing", time.Second)
	first.reply <- live
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:transcript-syncing") })
	f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "snapshot"}}}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	refresh := <-f.snapshots
	// Keep the refresh outstanding longer than the dots' debounce. The Agent
	// is still running, but history is complete and this is a live-tail read.
	time.Sleep(350 * time.Millisecond)
	app.Settle()
	if s.transcriptSyncing || app.Exists("id:transcript-syncing") {
		t.Fatal("live tail refresh was presented as history catch-up")
	}
	f.events <- Object{"epoch": "live", "cursor": 2, "events": []Object{{"id": 2, "kind": "text", "turnId": "reply", "itemId": "answer", "offset": 4, "delta": "，继续生成"}}}
	waitAnimatedText(app, "正在回复，继续生成", time.Second)
	if app.Exists("id:transcript-syncing") {
		t.Fatal("live text reactivated catch-up dots")
	}
	refresh.reply <- live
	app.WaitUntil(time.Second, func() bool { app.Settle(); return !s.stream.snapshotInFlight })
	// A genuine history reset must still show catch-up, even with a running
	// turn on screen; hiding every indicator for active turns would be wrong.
	f.events <- Object{"epoch": "live", "cursor": 2, "reset": true}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	reset := <-f.snapshots
	app.WaitFor("id:transcript-syncing", time.Second)
	reset.reply <- live
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:transcript-syncing") })
	app.Screenshot("caught-up-agent-still-replying")
}

func TestPartialHistoryPreservesCacheUntilAuthoritativeCompletion(t *testing.T) {
	old := Object{"turns": []Object{{"id": "older", "status": "completed", "items": []Object{{"id": "a", "text": "缓存旧消息"}}}, {"id": "current", "status": "completed", "items": []Object{{"id": "b", "text": "缓存第一条"}, {"id": "c", "text": "缓存第二条"}}}, {"id": "live", "status": "inProgress", "items": []Object{{"id": "d", "text": "新的实时回复"}}}}}
	partial := Object{"turns": []Object{{"id": "current", "status": "completed", "items": []Object{{"id": "b", "text": "服务器修正第一条"}}}}}
	for range 2 {
		old = previewTranscript(old, partial)
	}
	turns := objects(old["turns"])
	if len(turns) != 3 || str(turns[0], "id") != "older" || str(turns[1], "id") != "current" || str(turns[2], "id") != "live" || len(objects(turns[1]["items"])) != 2 || str(objects(turns[1]["items"])[0], "text") != "服务器修正第一条" {
		t.Fatalf("partial history erased or duplicated cache: %+v", old)
	}
}

func TestSmoothReplyOnlyAnimatesNewLiveSuffix(t *testing.T) {
	var text *core.Signal[string]
	var animate *core.Signal[bool]
	var unrelated *core.Signal[int]
	var scope *core.Scope
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		scope = cx
		cx.SetTheme(core.DefaultTheme())
		text = core.NewSignal(cx, "历史内容🙂")
		animate = core.NewSignal(cx, false)
		unrelated = core.NewSignal(cx, 0)
		shown := smoothReply(cx, text.Get, func() bool { unrelated.Get(); return animate.Get() })
		return core.Text{TestID: "reply", ContentFunc: shown}
	}, nsgotest.ManualFrames())
	app.Find("text:历史内容🙂")
	app.Post(func() { scope.Post(func() { text.Set("历史内容🙂这是新增的文字"); animate.Set(true) }) })
	app.Settle()
	if app.Find("id:reply").Text != "历史内容🙂" {
		t.Fatal("live suffix appeared in one jump:", app.Find("id:reply").Text)
	}
	app.Advance(66 * time.Millisecond)
	mid := app.Find("id:reply").Text
	if !strings.HasPrefix(mid, "历史内容🙂") || mid == "历史内容🙂" || mid == "历史内容🙂这是新增的文字" {
		t.Fatal("live suffix did not reveal progressively:", mid)
	}
	app.Post(func() { scope.Post(func() { unrelated.Set(1) }) })
	app.Advance(250 * time.Millisecond)
	app.Find("text:历史内容🙂这是新增的文字")
	// Synchronization/corrections bypass typing, even when replacing the prefix.
	app.Post(func() { scope.Post(func() { animate.Set(false); text.Set("同步补齐后的完整消息") }) })
	app.Settle()
	app.Find("text:同步补齐后的完整消息")
}

// Headless WaitFor settles posted work but intentionally does not take frames.
func waitAnimatedText(app *nsgotest.App, text string, timeout time.Duration) {
	app.WaitUntil(timeout, func() bool {
		app.Advance(250 * time.Millisecond)
		return app.Exists("text:" + text)
	})
}
