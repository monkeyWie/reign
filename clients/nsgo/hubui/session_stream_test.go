package hubui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type snapshotRequest struct{ reply chan Object }
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
			request := snapshotRequest{reply: make(chan Object, 1)}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case f.snapshots <- request:
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case t := <-request.reply:
				return t, nil
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
	app.Screenshot("sync-complete-live-reply")
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
