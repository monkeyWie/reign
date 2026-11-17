package codex

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

type ipcFixture struct {
	t        *testing.T
	b        *appIPC
	wire     net.Conn
	mu       sync.Mutex
	writes   sync.Mutex
	methods  []string
	callback func(codexipc.Message) (codexipc.Message, bool)
}

func (f *ipcFixture) send(m codexipc.Message) {
	f.writes.Lock()
	defer f.writes.Unlock()
	raw := rawJSON(m)
	head := make([]byte, 4)
	binary.LittleEndian.PutUint32(head, uint32(len(raw)))
	_ = f.wire.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := io.Copy(f.wire, io.MultiReader(strings.NewReader(string(head)), strings.NewReader(string(raw))))
	if err != nil && !errors.Is(err, net.ErrClosed) { /* shutdown can close an in-flight fixture broadcast */
	}
}
func attachIPCFixture(t *testing.T, p *Provider, callback func(codexipc.Message) (codexipc.Message, bool)) *ipcFixture {
	t.Helper()
	b := newAppIPC(p)
	b.home = t.TempDir()
	p.ipc = b
	a, wire := net.Pipe()
	f := &ipcFixture{t: t, b: b, wire: wire, callback: callback}
	go func() {
		for {
			m, err := codexipc.ReadFrame(wire)
			if err != nil {
				return
			}
			f.mu.Lock()
			f.methods = append(f.methods, m.Method)
			f.mu.Unlock()
			if m.Type != "request" && m.Type != "broadcast" {
				continue
			}
			res := codexipc.Message{Type: "response", RequestID: m.RequestID, Method: m.Method, ResultType: "success", Result: rawJSON(map[string]any{}), HandledBy: "app-owner"}
			if m.Method == "initialize" {
				res.Result = rawJSON(map[string]any{"clientId": "reign-test"})
				f.send(res)
				continue
			}
			if callback != nil {
				custom, handled := callback(m)
				if handled {
					custom.Type = "response"
					custom.RequestID = m.RequestID
					custom.Method = m.Method
					f.send(custom)
					continue
				}
			}
			if m.Type == "request" {
				f.send(res)
			}
		}
	}()
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	c, err := codexipc.Connect(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	b.conn = c
	go b.run()
	t.Cleanup(func() { _ = p.Close(); _ = wire.Close() })
	return f
}
func (f *ipcFixture) snapshot(id string, revision int64, text string) {
	state := map[string]any{"id": id, "title": "App session", "cwd": "/project", "latestModel": "native", "historyMode": "paginated", "turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{"isComplete": true, "entitiesByKey": map[string]any{"turn:t": map[string]any{"turnId": "t", "status": "inProgress", "items": []any{map[string]any{"id": "a", "type": "agentMessage", "text": text}}}}, "islands": []any{map[string]any{"entries": []any{map[string]any{"key": "turn:t", "value": "turn:t"}}}}}}, "requests": []any{}}
	f.send(codexipc.Message{Type: "broadcast", Method: "thread-stream-state-changed", Source: "app-owner", Version: 11, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": id, "change": map[string]any{"type": "snapshot", "revision": revision, "conversationState": state}})})
}
func backendFixture(t *testing.T, p *Provider, handle func(rpc.Message) (any, *rpc.Error)) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			_, raw, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var m rpc.Message
			_ = json.Unmarshal(raw, &m)
			if m.Method == "initialized" {
				continue
			}
			var out any = map[string]any{}
			var rpcErr *rpc.Error
			if m.Method != "initialize" {
				out, rpcErr = handle(m)
			}
			reply := map[string]any{"id": m.ID, "result": out}
			if rpcErr != nil {
				reply = map[string]any{"id": m.ID, "error": rpcErr}
			}
			if ws.Write(r.Context(), websocket.MessageText, rawJSON(reply)) != nil {
				return
			}
		}
	}))
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	c, err := rpc.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), p.observe)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	p.client = c
	t.Cleanup(func() { _ = c.Close(); server.Close() })
}
func TestIPCAppSendAndWatchNeverResumeExternalThread(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		t.Errorf("external thread reached private backend: %s", m.Method)
		return nil, nil
	})
	var f *ipcFixture
	f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-stream-following-changed" {
			go f.snapshot("external", 1, "hello")
		}
		return codexipc.Message{}, false
	})
	ctx, stop := context.WithTimeout(context.Background(), 4*time.Second)
	defer stop()
	if _, err := p.Probe(ctx, "external", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := p.Send(ctx, "external", "continue"); err != nil {
		t.Fatal(err)
	}
	out, err := p.Transcript(ctx, "external", "")
	if err != nil || len(out.Turns) != 1 || out.Turns[0].Items[0].Text != "hello" {
		t.Fatalf("IPC transcript: %+v %v", out, err)
	}
	f.mu.Lock()
	methods := strings.Join(f.methods, ",")
	f.mu.Unlock()
	if !strings.Contains(methods, "thread-follower-start-turn") {
		t.Fatal(methods)
	}
}
func TestIPCUnknownSendOutcomeDoesNotFallbackOrReplay(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) { t.Errorf("unsafe fallback: %s", m.Method); return nil, nil })
	var f *ipcFixture
	f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-stream-following-changed" {
			go f.snapshot("external", 1, "")
		}
		if m.Method == "thread-follower-start-turn" {
			return codexipc.Message{ResultType: "error", Error: "request-timeout"}, true
		}
		return codexipc.Message{}, false
	})
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	err := p.Send(ctx, "external", "execute once")
	var unknown *codexipc.UnknownOutcome
	if !errors.As(err, &unknown) {
		t.Fatalf("outcome misclassified: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, m := range f.methods {
		if m == "thread-follower-start-turn" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("replayed %d times", count)
	}
}
func TestIPCSnapshotRejectsWrongOwnerAndUnknownVersion(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	b := newAppIPC(p)
	defer b.cancel()
	p.ipc = b
	b.followed["s"] = &followedThread{owner: "owner", changed: make(chan struct{})}
	state := map[string]any{"id": "s", "turns": []any{}}
	message := func(source string, version int, revision int64) codexipc.Message {
		return codexipc.Message{Method: "thread-stream-state-changed", Source: source, Version: version, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": map[string]any{"type": "snapshot", "revision": revision, "conversationState": state}})}
	}
	b.Broadcast(message("wrong", 11, 1))
	if len(b.followed["s"].state) != 0 {
		t.Fatal("unsolicited owner accepted")
	}
	b.Broadcast(message("owner", 99, 1))
	if b.followed["s"].err == nil {
		t.Fatal("unknown schema silently accepted")
	}
	b.Broadcast(message("owner", 11, 2))
	if b.followed["s"].err != nil || b.followed["s"].revision != 2 {
		t.Fatal("supported snapshot failed")
	}
	b.Broadcast(message("owner", 11, 1))
	if b.followed["s"].revision != 2 {
		t.Fatal("old snapshot rolled state back")
	}
}
func TestIPCPatchesUnicodeAndTransactionalRecovery(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	f := attachIPCFixture(t, p, nil)
	b := f.b
	b.mu.Lock()
	b.followed["s"] = &followedThread{owner: "app-owner", changed: make(chan struct{})}
	b.mu.Unlock()
	f.snapshot("s", 1, "你😀")
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if _, err := b.waitState(ctx, "s", "app-owner"); err != nil {
		t.Fatal(err)
	}
	epoch, cursor := p.stream().log.Position()
	b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Source: "app-owner", Version: 11, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{map[string]any{"op": "replace", "path": "/turnHistory/history/entitiesByKey/turn:t/items/0/text", "value": "你😀好"}}}})})
	batch, err := p.stream().log.Probe(ctx, "s", epoch, cursor)
	if err != nil || len(batch.Events) == 0 || batch.Events[len(batch.Events)-1].Kind != "text" || batch.Events[len(batch.Events)-1].Offset != 3 || batch.Events[len(batch.Events)-1].Delta != "好" {
		t.Fatalf("bad UTF16 delta: %+v %v", batch, err)
	}
	b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Source: "app-owner", Version: 11, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": map[string]any{"type": "patches", "baseRevision": 0, "revision": 3, "patches": []any{}}})})
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.followed["s"].revision != 2 || b.followed["s"].err == nil {
		t.Fatal("patch gap applied without snapshot")
	}
}
func TestIPCOwnerQueueDeduplicatesAndPreservesInput(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	started := make(chan map[string]any, 5)
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		if m.Method == "turn/start" {
			started <- decodeObject(m.Params)
			return map[string]any{"turn": map[string]any{"id": "t", "status": "inProgress", "items": []any{}}}, nil
		}
		return nil, nil
	})
	f := attachIPCFixture(t, p, nil)
	b := f.b
	b.register("owned", rawJSON(map[string]any{"thread": map[string]any{"id": "owned", "cwd": "/p"}}), true)
	imagePath := filepath.Join(t.TempDir(), "photo.png")
	message := map[string]any{"id": "msg1", "text": "from App", "context": map[string]any{"attachments": []any{map[string]any{"type": "image", "path": imagePath}}}}
	request := codexipc.Message{Type: "request", Method: "thread-follower-set-queued-follow-ups-state", Version: 3, Source: "app", Params: rawJSON(map[string]any{"conversationId": "owned", "state": map[string]any{"owned": []any{message}}})}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if _, err := b.Handle(ctx, request); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-started:
		if len(array(input["input"])) != 2 || input["clientUserMessageId"] != "msg1" {
			t.Fatal(input)
		}
		if image := object(array(input["input"])[1]); image["type"] != "localImage" || image["path"] != imagePath {
			t.Fatalf("queued image input changed: %+v", image)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Wait for the acknowledged queue removal, then resend the same App state.
	for {
		b.mu.Lock()
		sent := b.owned["owned"].sent["msg1"]
		b.mu.Unlock()
		if sent {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := b.Handle(ctx, request); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	o := b.owned["owned"]
	if len(o.queue) != 0 {
		t.Fatal("acknowledged message requeued")
	}
	b.mu.Unlock()
	select {
	case <-started:
		t.Fatal("same App message executed twice")
	default:
	}
}
func TestReadOnlyUnownedIPCWatchDoesNotAcquireWriter(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) { t.Errorf("read acquired writer: %s", m.Method); return nil, nil })
	attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-owner-discovery" {
			return codexipc.Message{ResultType: "error", Error: "no-client-found"}, true
		}
		return codexipc.Message{}, false
	})
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if _, err := p.Probe(ctx, "historical", "", 0); err != nil {
		t.Fatal(err)
	}
}

func TestIPCDiffRoundtripAndGranularTextPatches(t *testing.T) {
	old := decodeObject(rawJSON(map[string]any{"a/b~c": map[string]any{"deleted": true}, "items": []any{map[string]any{"id": "a", "text": "before"}, map[string]any{"id": "b", "text": "keep"}}}))
	next := decodeObject(rawJSON(map[string]any{"a/b~c": map[string]any{"new": true}, "items": []any{map[string]any{"id": "a", "text": "after"}, map[string]any{"id": "b", "text": "keep"}, map[string]any{"id": "c"}}}))
	patches := []any{}
	diffState([]any{}, old, next, &patches)
	encoded := rawJSON(patches)
	patch, err := decodeIPCPatches(encoded)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := patch.Apply(rawJSON(old))
	if err != nil || !reflect.DeepEqual(decodeObject(applied), next) {
		t.Fatal(string(encoded), string(applied), err)
	}
	if !strings.Contains(string(encoded), `["items",0,"text"]`) || !strings.Contains(string(encoded), `["a/b~c","new"]`) {
		t.Fatal("whole turn replaced or path not an Immer array", string(encoded))
	}
	patches = nil
	diffState([]any{}, next, old, &patches)
	patch, err = decodeIPCPatches(rawJSON(patches))
	if err != nil {
		t.Fatal(err)
	}
	applied, err = patch.Apply(rawJSON(next))
	if err != nil || !reflect.DeepEqual(decodeObject(applied), old) {
		t.Fatal("array shrink failed", string(applied), err)
	}
}

func TestIPCOwnerRejectsReusedMessageIDAndAnswersApprovalOnce(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	var starts atomic.Int32
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		starts.Add(1)
		return map[string]any{"turn": map[string]any{"id": "t", "status": "inProgress", "items": []any{}}}, nil
	})
	f := attachIPCFixture(t, p, nil)
	b := f.b
	b.register("owned", rawJSON(map[string]any{"thread": map[string]any{"id": "owned"}}), true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	params := map[string]any{"threadId": "owned", "clientUserMessageId": "once", "input": []any{map[string]any{"type": "text", "text": "first"}}}
	var first, second json.RawMessage
	if err := b.startFromApp(ctx, "owned", params, &first); err != nil {
		t.Fatal(err)
	}
	if err := b.startFromApp(ctx, "owned", params, &second); err != nil || string(first) != string(second) || starts.Load() != 1 {
		t.Fatal("duplicate executed", err, starts.Load())
	}
	params["input"] = []any{map[string]any{"type": "text", "text": "changed"}}
	if err := b.startFromApp(ctx, "owned", params, &second); err == nil || starts.Load() != 1 {
		t.Fatal("ID reuse accepted", err)
	}
	var replies atomic.Int32
	p.addPending(rpc.Message{ID: rawJSON("approval"), Method: "item/permissions/requestApproval", Params: rawJSON(map[string]any{"threadId": "owned"})}, func(context.Context, any) error { replies.Add(1); return nil }, "service")
	approval := map[string]any{"requestId": "approval", "response": map[string]any{"permissions": map[string]any{}, "scope": "turn"}}
	if err := b.answerOwnerApproval(ctx, "owned", "thread-follower-permissions-request-approval-response", approval); err != nil {
		t.Fatal(err)
	}
	if err := b.answerOwnerApproval(ctx, "owned", "thread-follower-permissions-request-approval-response", approval); err == nil || replies.Load() != 1 {
		t.Fatal("approval answered twice", err)
	}
}

func TestIPCHistoryReadCannotRollBackLiveTail(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	reading, release := make(chan struct{}), make(chan struct{})
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		if m.Method == "thread/read" {
			return map[string]any{"thread": map[string]any{"id": "owned", "name": "old"}}, nil
		}
		if m.Method == "thread/turns/list" {
			close(reading)
			<-release
			return map[string]any{"data": []any{map[string]any{"id": "t", "status": "inProgress", "items": []any{map[string]any{"id": "a", "type": "agentMessage", "text": "old"}}}}}, nil
		}
		return nil, nil
	})
	f := attachIPCFixture(t, p, nil)
	b := f.b
	b.register("owned", rawJSON(map[string]any{"thread": map[string]any{"id": "owned"}}), false)
	b.observe(rpc.Message{Method: "item/started", Params: rawJSON(map[string]any{"threadId": "owned", "turnId": "t", "item": map[string]any{"id": "a", "type": "agentMessage", "text": "old"}})})
	done := make(chan error, 1)
	go func() { done <- b.loadOwned(context.Background(), "owned", false) }()
	<-reading
	b.observe(rpc.Message{Method: "item/agentMessage/delta", Params: rawJSON(map[string]any{"threadId": "owned", "turnId": "t", "itemId": "a", "delta": " new"})})
	b.observe(rpc.Message{Method: "thread/name/updated", Params: rawJSON(map[string]any{"threadId": "owned", "name": "new title"})})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.owned["owned"]
	if object(array(o.turns[0]["items"])[0])["text"] != "old new" || o.metadata["name"] != "new title" {
		t.Fatal(o)
	}
}

func TestIPCOwnerPublishesFreshSnapshotToNewFollower(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	backendFixture(t, p, func(rpc.Message) (any, *rpc.Error) {
		t.Error("loaded state unexpectedly reread history")
		return nil, nil
	})
	changes := make(chan map[string]any, 8)
	f := attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-stream-state-changed" {
			changes <- object(decodeObject(m.Params)["change"])
		}
		return codexipc.Message{}, false
	})
	b := f.b
	b.register("owned", rawJSON(map[string]any{"thread": map[string]any{"id": "owned", "cwd": "/p"}}), true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	follow := func(source string) {
		b.Broadcast(codexipc.Message{Method: "thread-stream-following-changed", Version: 1, Source: source, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "owned", "following": true})})
	}
	next := func() map[string]any {
		select {
		case c := <-changes:
			return c
		case <-ctx.Done():
			t.Fatal(ctx.Err())
			return nil
		}
	}
	follow("app1")
	if err := b.publish(ctx, "owned", false); err != nil {
		t.Fatal(err)
	}
	if c := next(); c["type"] != "snapshot" {
		t.Fatal(c)
	}
	b.mu.Lock()
	b.owned["owned"].metadata["name"] = "renamed"
	b.mu.Unlock()
	if err := b.publish(ctx, "owned", false); err != nil {
		t.Fatal(err)
	}
	if c := next(); c["type"] != "patches" {
		t.Fatal(c)
	}
	follow("app2")
	if err := b.publish(ctx, "owned", false); err != nil {
		t.Fatal(err)
	}
	if c := next(); c["type"] != "snapshot" {
		t.Fatal("new follower received a patch without baseline", c)
	}
}

func TestIPCHistoryFailureResumesFromCommittedPage(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	var newest, older atomic.Int32
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		if m.Method == "thread/read" {
			return map[string]any{"thread": map[string]any{"id": "owned"}}, nil
		}
		if m.Method == "thread/turns/list" {
			params := decodeObject(m.Params)
			if params["cursor"] == "old-page" {
				if older.Add(1) == 1 {
					return nil, &rpc.Error{Code: -32000, Message: "temporary read failure"}
				}
				return map[string]any{"data": []any{map[string]any{"id": "older", "status": "completed", "items": []any{}}}}, nil
			}
			newest.Add(1)
			return map[string]any{"data": []any{map[string]any{"id": "latest", "status": "completed", "items": []any{}}}, "nextCursor": "old-page"}, nil
		}
		return nil, nil
	})
	f := attachIPCFixture(t, p, nil)
	b := f.b
	b.register("owned", rawJSON(map[string]any{"thread": map[string]any{"id": "owned"}}), false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.loadOwned(ctx, "owned", true); err == nil {
		t.Fatal("failed page reported success")
	}
	b.mu.Lock()
	cursor := b.owned["owned"].metadata["_olderCursor"]
	count := len(b.owned["owned"].turns)
	b.mu.Unlock()
	if cursor != "old-page" || count != 1 {
		t.Fatal("successful page not committed", cursor, count)
	}
	if err := b.loadOwned(ctx, "owned", true); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	turns := b.owned["owned"].turns
	if newest.Load() != 1 || older.Load() != 2 || len(turns) != 2 || turns[0]["id"] != "older" || turns[1]["id"] != "latest" {
		t.Fatal("history restarted or reordered", newest.Load(), older.Load(), turns)
	}
}
