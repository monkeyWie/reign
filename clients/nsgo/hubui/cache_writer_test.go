package hubui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/persistence"
)

type capturedSnapshot struct {
	params  Object
	release chan struct{}
}

type snapshotTestAPI struct{ calls chan capturedSnapshot }

func (*snapshotTestAPI) CanHost() bool { return false }
func (*snapshotTestAPI) Close()        {}
func (a *snapshotTestAPI) Call(ctx context.Context, _ string, params Object, _ string, _ Object) (any, error) {
	call := capturedSnapshot{params: params, release: make(chan struct{})}
	select {
	case a.calls <- call:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-call.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestSnapshotWriterCapturesOverlayAndCoalescesHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	api := &snapshotTestAPI{calls: make(chan capturedSnapshot)}
	s := &State{snapshots: newSnapshotWriter(ctx, api)}
	pending := &outgoingMessage{ID: "pending", Text: "draft", Status: "sending", Known: map[string]bool{"old": true}, Anchors: []string{"old"}}
	history := Object{"turns": []Object{{"id": "live", "status": "inProgress", "items": []Object{{"id": "answer", "text": "a"}}}}, "localMessages": []*outgoingMessage{pending}, "resume": Object{"epoch": "e", "cursor": 1}}
	nextCall := func() capturedSnapshot {
		t.Helper()
		select {
		case call := <-api.calls:
			return call
		case <-time.After(time.Second):
			t.Fatal("snapshot writer stalled")
			return capturedSnapshot{}
		}
	}
	s.writeSnapshot("local", "codex-app", "chat", history)
	first := nextCall()
	// Simulate mutable delivery state changing while the adapter is delayed.
	pending.Status, pending.Known["new"], pending.Anchors[0] = "uncertain", true, "new"
	for i := 1; i <= 20; i++ {
		history, _ = applyEvents(history, []Object{{"kind": "text", "turnId": "live", "itemId": "answer", "offset": i, "delta": "a"}})
		history["resume"] = Object{"epoch": "e", "cursor": i + 1}
		s.writeSnapshot("local", "codex-app", "chat", history)
	}
	// Exercise the adapter's serialization after UI updates, including concrete
	// outgoing structs; it must still see the content/cursor captured together.
	data, err := json.Marshal(first.params["transcript"])
	if err != nil {
		t.Fatal(err)
	}
	var saved Object
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	message := objects(saved["localMessages"])[0]
	if str(message, "status") != "sending" || flag(object(message["known"]), "new") || message["anchors"].([]any)[0] != "old" {
		t.Fatal("pending delivery state mutated an in-flight snapshot")
	}
	if number(object(saved["resume"]), "cursor") != 1 || str(objects(objects(saved["turns"])[0]["items"])[0], "text") != "a" {
		t.Fatal("live changes mutated persisted content or cursor")
	}
	close(first.release)
	last := nextCall()
	if number(object(object(last.params["transcript"])["resume"]), "cursor") != 21 {
		t.Fatal("writer did not coalesce the delayed checkpoints")
	}
	cancel() // A blocked adapter is cancelled with the writer's owner.
}

type historySendTestAPI struct {
	*delayedSendAPI
	persistence.HistoryDAO
}

func (a *historySendTestAPI) Call(ctx context.Context, method string, params Object, target string, request Object) (any, error) {
	if method == "client.call" && str(request, "method") == "transcript" {
		if str(request, "cursor") != "" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		t := outgoingTranscript(Object{"id": "prior", "role": "user", "text": "earlier request"}, Object{"id": "acknowledged", "role": "user", "text": "written once"})
		t["nextCursor"] = "earlier"
		return t, nil
	}
	return a.delayedSendAPI.Call(ctx, method, params, target, request)
}

func TestSendingWhileOlderHistoryCommits(t *testing.T) {
	dao := timelineTestDAO(t)
	ctx := context.Background()
	api := &historySendTestAPI{&delayedSendAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}, dao}
	key := persistence.Session{Env: "demo-device", Provider: "codex-app", ID: "demo-chat"}
	base := outgoingTranscript(Object{"id": "prior", "role": "user", "text": "earlier request"})
	base["nextCursor"] = "earlier"
	app, s := conversationFixture(t, api, 390, base, true)
	app.Post(func() { s.cx.Post(s.persistSnapshot) })
	app.WaitUntil(3*time.Second, func() bool { return str(s.transcript, "_historyRevision") != "" })
	page, err := dao.ReadPage(ctx, key, persistence.PageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	older := persistence.WithOrigin(Object{"turns": []Object{{"id": "old", "items": []Object{{"id": "first", "role": "user", "text": "first request"}}}}}, persistence.PageCursor{Turn: -1}, false)
	patch := persistence.Diff(page.Revision, nil, older)
	patch.Revision = persistence.OlderPageRevision(page.Revision)
	patch.Replace = false
	patch.Head = Object{"nextCursor": "even-earlier"}
	if err := dao.ApplyHistory(ctx, key, patch); err != nil {
		t.Fatal(err)
	}
	app.Type("id:chat-input", "written once")
	app.Press("id:send-message")
	app.WaitFor("id:send-loading", time.Second)
	// Inspect the actual Send action while the Agent acknowledgement is held.
	app.WaitUntil(3*time.Second, func() bool {
		p, e := dao.ReadPage(ctx, key, persistence.PageQuery{})
		return e == nil && (len(objects(p.Transcript["localMessages"])) == 1 || s.notice != "")
	})
	if s.notice != "" {
		t.Fatalf("local send raised %q", s.notice)
	}
	close(api.release)
	app.WaitUntil(3*time.Second, func() bool {
		p, e := dao.ReadPage(ctx, key, persistence.PageQuery{})
		if e != nil {
			return false
		}
		rows := messages(p.Transcript)
		return len(objects(p.Transcript["localMessages"])) == 0 && len(rows) > 0 && rows[len(rows)-1].Text == "written once"
	})
	p, e := dao.ReadPage(ctx, key, persistence.PageQuery{})
	if e != nil {
		t.Fatal(e)
	}
	turns := objects(p.Transcript["turns"])
	if api.sends.Load() != 1 || len(turns) < 2 || str(turns[0], "id") != "old" || str(p.Transcript, "nextCursor") != "even-earlier" {
		t.Fatalf("send count %d, turns %d, paging cursor %q", api.sends.Load(), len(turns), str(p.Transcript, "nextCursor"))
	}
}

func TestStaleSenderPreservesCommittedHistoryAndOtherDeliveries(t *testing.T) {
	ctx := context.Background()
	dao := timelineTestDAO(t)
	w := snapshotWriter{storage: dao}
	key := persistence.Session{Env: "fixture-" + persistence.Revision(), Provider: "fixture", ID: "stale"}
	old := outgoingTranscript(Object{"id": "u", "role": "user", "text": "first"})
	old["resume"] = Object{"epoch": "test", "cursor": 1}
	old["localMessages"] = []Object{{"id": "confirmed-elsewhere", "status": "sending"}}
	previous, err := w.save(ctx, key, nil, snapshotWrite{transcript: old})
	if err != nil {
		t.Fatal(err)
	}
	current := outgoingTranscript(Object{"id": "u", "role": "user", "text": "first"}, Object{"id": "reply", "role": "assistant", "text": "newer reply"})
	current["resume"] = Object{"epoch": "test", "cursor": 3}
	current["localMessages"] = []Object{{"id": "another-tab", "status": "accepted"}}
	current["queue"] = Object{"messages": []Object{{"id": "queued", "text": "queued elsewhere"}}}
	if e := dao.ApplyHistory(ctx, key, persistence.Diff(str(previous, "_historyRevision"), old, current)); e != nil {
		t.Fatal(e)
	}
	stale := copyObject(old)
	stale["localMessages"] = []Object{{"id": "confirmed-elsewhere", "status": "accepted"}, {"id": "mine", "status": "accepted"}}
	saved, err := w.save(ctx, key, previous, snapshotWrite{transcript: stale})
	if err != nil || !flag(saved, "_refreshHistory") {
		t.Fatalf("competing live update: reconciliation %v, error %v", flag(saved, "_refreshHistory"), err)
	}
	// Repeat the same delayed callback after the storage revision is up to date.
	if _, err = w.save(ctx, key, saved, snapshotWrite{transcript: stale}); err != nil {
		t.Fatal(err)
	}
	page, err := dao.ReadPage(ctx, key, persistence.PageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	v := page.Transcript
	rows := messages(v)
	states := map[string]string{}
	for _, m := range objects(v["localMessages"]) {
		states[str(m, "id")] = str(m, "status")
	}
	if len(rows) != 2 || rows[1].Text != "newer reply" || number(object(v["resume"]), "cursor") != 3 || len(objects(object(v["queue"])["messages"])) != 1 || len(states) != 2 || states["mine"] != "accepted" || states["another-tab"] != "accepted" {
		t.Fatalf("unexpected saved history: %d rows, cursor %d, deliveries %v", len(rows), number(object(v["resume"]), "cursor"), states)
	}
}

type failingHistorySendAPI struct{ *historySendTestAPI }

func (*failingHistorySendAPI) ApplyHistory(context.Context, persistence.Session, persistence.HistoryPatch) error {
	return errors.New("fixture storage failure")
}

func TestSendReportsGenuineStorageFailure(t *testing.T) {
	api := &failingHistorySendAPI{&historySendTestAPI{&delayedSendAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}, timelineTestDAO(t)}}
	app, s := conversationFixture(t, api, 390, nil, true)
	app.Type("id:chat-input", "written once")
	app.Press("id:send-message")
	app.WaitFor("id:send-loading", time.Second)
	close(api.release)
	app.WaitUntil(3*time.Second, func() bool {
		return !app.Exists("id:send-loading") && s.notice == "此会话的本地记录未能保存"
	})
	if api.sends.Load() != 1 || s.error != "" {
		t.Fatal("storage failure changed Agent delivery or repeated the send")
	}
}
