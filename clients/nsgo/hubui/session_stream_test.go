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
	// Establish the history ordering boundary before testing a slow refresh.
	request.reply <- Object{"streamEpoch": "live", "streamCursor": 0, "turns": objects(cached["turns"])}
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.stream.baseline })
	f.events <- Object{"epoch": "live", "cursor": 0, "events": []Object{{"kind": "snapshot"}}}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	request = <-f.snapshots
	f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "text", "turnId": "reply", "itemId": "answer", "offset": 0, "delta": "第一段🙂"}}}
	waitAnimatedText(app, "第一段🙂", time.Second)
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
	if f.reads.Load() != 2 {
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
			s.refreshAfterSend(s.key(), s.target, s.stream.provider, s.stream.session, false)
			if s.stream.snapshotInFlight || s.stream.snapshotAgain {
				t.Error("successful send restarted a joined history snapshot")
			}
			checked.Store(true)
		})
	})
	app.WaitUntil(time.Second, checked.Load)
	app.Screenshot("sync-complete-live-reply")
}

func TestRunningOwnerReplacesTerminalCacheBeforeCompletion(t *testing.T) {
	for _, fromSnapshot := range []bool{true, false} {
		t.Run(choose(fromSnapshot, "snapshot", "turn-event"), func(t *testing.T) {
			f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
			cached := Object{"turns": []Object{{"id": "reply", "status": "completed", "items": []Object{
				{"id": "question", "role": "assistant", "phase": "final_answer", "text": "选择一个方案"},
				{"id": "steer", "role": "user", "text": "继续处理"},
			}}}}
			if !fromSnapshot {
				cached["resume"] = Object{"epoch": "live", "cursor": 1}
			}
			app, s := conversationFixture(t, f, 390, cached, true)
			app.Post(func() { s.cx.Post(s.startSessionWatch) })
			if fromSnapshot {
				app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
				request := <-f.snapshots
				request.reply <- Object{"streamEpoch": "live", "streamCursor": 0, "turns": []Object{{"id": "reply", "status": "inProgress", "items": []Object{
					{"id": "question", "role": "assistant", "phase": "final_answer", "text": "选择一个方案"},
					{"id": "steer", "role": "user", "text": "继续处理"},
					{"id": "progress", "role": "assistant", "phase": "commentary", "status": "inProgress", "text": "开始检查"},
				}}}}
			} else {
				f.events <- Object{"epoch": "live", "cursor": 3, "events": []Object{
					{"id": 2, "kind": "turn", "turnId": "reply", "status": "inProgress"},
					{"id": 3, "kind": "item", "turnId": "reply", "itemId": "progress", "item": Object{"id": "progress", "role": "assistant", "phase": "commentary", "status": "inProgress", "text": "开始检查"}},
				}}
			}
			app.WaitFor("text:开始检查", time.Second)
			if activeTurn(s.transcript) == nil {
				t.Fatal("current owner could not restore the running turn")
			}
			f.events <- Object{"epoch": "live", "cursor": 4, "events": []Object{{"id": 4, "kind": "text", "turnId": "reply", "itemId": "progress", "offset": 4, "delta": "，仍在执行🙂"}}}
			waitAnimatedText(app, "开始检查，仍在执行🙂", time.Second)
			app.Find("text:继续处理")
			for _, row := range s.messageRows {
				if row.Role == "work" {
					t.Fatal("running messages were folded as completed work")
				}
			}
			app.Screenshot("running-owner-restored-" + choose(fromSnapshot, "snapshot", "event"))
		})
	}
}

func TestDelayedRunningSnapshotReplaysCompletionBeforeMerging(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	cached := Object{"resume": Object{"epoch": "live", "cursor": 1}, "turns": []Object{{"id": "reply", "status": "inProgress", "items": []Object{{"id": "answer", "role": "assistant", "text": "尚未完成"}}}}}
	app, s := conversationFixture(t, f, 390, cached, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.stream != nil && s.stream.baseline })
	f.events <- Object{"epoch": "live", "cursor": 2, "events": []Object{{"id": 2, "kind": "snapshot"}}}
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	request := <-f.snapshots
	f.events <- Object{"epoch": "live", "cursor": 4, "events": []Object{
		{"id": 3, "kind": "item", "turnId": "reply", "itemId": "answer", "item": Object{"id": "answer", "role": "assistant", "status": "completed", "text": "最终结果"}},
		{"id": 4, "kind": "turn", "turnId": "reply", "status": "completed"},
	}}
	app.WaitFor("text:最终结果", time.Second)
	// This response was captured before completion, but arrives after it.
	request.reply <- Object{"streamEpoch": "live", "streamCursor": 2, "turns": objects(cached["turns"])}
	// Completion requests one authoritative reconciliation after the old read.
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	app.Find("text:最终结果")
	if activeTurn(s.transcript) != nil {
		t.Fatal("late history resurrected a completed turn")
	}
	final := <-f.snapshots
	final.reply <- Object{"streamEpoch": "live", "streamCursor": 4, "turns": []Object{{"id": "reply", "status": "completed", "items": []Object{{"id": "answer", "role": "assistant", "status": "completed", "text": "最终结果"}}}}}
	app.WaitUntil(time.Second, func() bool { app.Settle(); return !s.stream.snapshotInFlight && s.streamCursor == 4 })
	f.events <- Object{"epoch": "live", "cursor": 5, "events": []Object{{"id": 5, "kind": "text", "turnId": "reply", "itemId": "answer", "offset": 4, "delta": "过期片段"}}}
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.streamCursor == 5 })
	app.Find("text:最终结果")
	if activeTurn(s.transcript) != nil || app.Exists("text:最终结果过期片段") {
		t.Fatal("late delta resurrected a completed reply")
	}
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
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.stream.cursor == 1 })
	if app.Exists("text:同步期间的实时回复") {
		t.Fatal("live tail overtook unfinished ordered history")
	}
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
	current := copyObject(live)
	current["streamCursor"] = 2 // This history is read after the reset boundary.
	reset.reply <- current
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

func TestPartialHistoryInsertsMissingReplyBeforeLaterCachedUserItems(t *testing.T) {
	old := Object{"turns": []Object{{"id": "turn", "status": "completed", "items": []Object{{"id": "u1", "role": "user", "text": "first"}, {"id": "u2", "role": "user", "text": "second"}}}}}
	partial := Object{"turns": []Object{{"id": "turn", "status": "completed", "items": []Object{{"id": "u1", "role": "user", "text": "first"}, {"id": "answer", "role": "assistant", "phase": "commentary", "text": "reply prefix"}}}}}
	rows := messages(previewTranscript(old, partial))
	if len(rows) != 3 || rows[0].Text != "first" || rows[1].Text != "reply prefix" || rows[2].Text != "second" {
		t.Fatal("missing assistant reply was hidden or appended after later user messages", rows)
	}
}

func TestLongHistoryReplyRendersItsPrefixAndKeepsFollowingMessagesInOrder(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	app, s := conversationFixture(t, f, 390, Object{}, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	request := <-f.snapshots
	prefix, suffix := strings.Repeat("渐进回复 ", 30), strings.Repeat("后续正文 ", 300)
	fragment := provider.Transcript{StreamEpoch: "live", Turns: []provider.Turn{{ID: "history", Status: "completed", Items: []provider.Item{{ID: "user", Role: "user", Text: "历史问题"}, {ID: "answer", Role: "assistant", Phase: "commentary", Text: prefix, TextOffset: new(0)}}}}}
	request.fragments <- fragment
	app.WaitFor("text:"+strings.TrimSpace(prefix), time.Second)
	app.Find("text:历史问题")
	app.WaitFor("id:transcript-syncing", time.Second)
	app.Screenshot("assistant-history-first-prefix")
	f.events <- Object{"epoch": "live", "cursor": 1, "events": []Object{{"id": 1, "kind": "item", "turnId": "following", "itemId": "new-user", "item": Object{"id": "new-user", "role": "user", "text": "后来的问题"}}}}
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.stream.cursor == 1 })
	if app.Exists("text:后来的问题") {
		t.Fatal("small later user event overtook the unfinished reply")
	}
	fragment.Turns[0].Items = []provider.Item{{ID: "answer", Role: "assistant", Phase: "commentary", Text: suffix, TextOffset: new(len(prefix))}}
	request.fragments <- fragment
	app.WaitFor("text:"+strings.TrimSpace(prefix+suffix), time.Second)
	if app.Exists("text:后来的问题") {
		t.Fatal("history preview replayed a later event before ordered catch-up completed")
	}
	request.reply <- Object{"streamEpoch": "live", "streamCursor": 0, "turns": []Object{{"id": "history", "status": "completed", "items": []Object{{"id": "user", "role": "user", "text": "历史问题"}, {"id": "answer", "role": "assistant", "phase": "commentary", "text": prefix + suffix}}}}}
	app.WaitFor("text:后来的问题", time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:transcript-syncing") })
	app.Post(func() {
		s.cx.Post(func() {
			rows := s.messageRows
			if len(rows) != 3 || rows[0].Text != "历史问题" || rows[1].Text != prefix+suffix || rows[2].Text != "后来的问题" {
				t.Error("completed catch-up changed conversation order", rows)
			}
		})
	})
	app.Settle()
	app.Screenshot("ordered-progressive-assistant-history")
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

func TestSavedSessionCursorResumesWithMatchingContent(t *testing.T) {
	f := &independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}
	cached := Object{"resume": Object{"epoch": "live", "cursor": int64(10)}, "turns": []Object{{"id": "reply", "status": "inProgress", "items": []Object{{"id": "answer", "type": "agentMessage", "role": "assistant", "text": "saved"}}}}}
	app, s := conversationFixture(t, f, 390, cached, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitUntil(time.Second, func() bool { return s.stream != nil && s.stream.baseline && len(f.snapshots) > 0 })
	if f.reads.Load() != 1 || app.Exists("id:transcript-syncing") {
		t.Fatal("saved baseline must remain readable during one independent revalidation")
	}
	request := <-f.snapshots
	f.events <- Object{"epoch": "live", "cursor": int64(11), "events": []Object{{"id": int64(11), "kind": "text", "sessionId": "demo-chat", "turnId": "reply", "itemId": "answer", "offset": 5, "delta": " tail"}}}
	waitAnimatedText(app, "saved tail", time.Second)
	// The independent read predates the delta. Preserve it by replaying the
	// journal, then checkpoint matching content/cursor only after completion.
	request.reply <- Object{"streamEpoch": "live", "streamCursor": int64(10), "turns": objects(cached["turns"])}
	app.WaitUntil(time.Second, func() bool { return s.streamCursor == 11 && !s.stream.snapshotInFlight })
	completed := make(chan struct{}, 1)
	app.Post(func() {
		s.cx.Post(func() {
			snapshot := s.snapshotProjection()
			if number(object(snapshot["resume"]), "cursor") != 11 || str(objects(objects(snapshot["turns"])[0]["items"])[0], "text") != "saved tail" {
				t.Error("content and cursor separated", snapshot)
			}
			// Partial history cannot commit a cursor against unfinished content.
			s.stream.snapshotInFlight = true
			if s.snapshotProjection()["resume"] != nil {
				t.Error("partial baseline retained a durable cursor")
			}
			s.stream.snapshotInFlight = false
			completed <- struct{}{}
		})
	})
	app.WaitUntil(time.Second, func() bool {
		select {
		case <-completed:
			return true
		default:
			return false
		}
	})
}
