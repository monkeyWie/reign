package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestIPCVerifiedSchemasMergeHistoryAndStreamImmerPatches(t *testing.T) {
	for _, version := range []int{11, 13} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			p := New(config.Config{}, "", events.New())
			b := newAppIPC(p)
			defer b.cancel()
			p.ipc = b
			b.followed["s"] = &followedThread{owner: "app", changed: make(chan struct{})}
			state := decodeObject(rawJSON(map[string]any{
				"id": "s", "title": "App conversation", "cwd": "/project", "updatedAt": "2026-11-17T01:02:03Z",
				"turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{
					"isComplete": true,
					"entitiesByKey": map[string]any{"turn:t": map[string]any{"turnId": "t", "status": "inProgress", "turnStartedAtMs": "1794877323000", "items": []any{
						map[string]any{"id": "u", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "question"}}},
						map[string]any{"id": "a", "type": "agentMessage", "text": "old"},
					}}},
					"islands": []any{map[string]any{"entries": []any{map[string]any{"value": "turn:t"}}}},
				}},
				"turns": []any{map[string]any{"turnId": "t", "status": "inProgress", "items": []any{map[string]any{"id": "a", "type": "agentMessage", "text": "你😀"}}}},
			}))
			broadcast := func(change any) {
				b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Source: "app", Version: version, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": change})})
			}
			broadcast(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state})
			transcript, err := p.Transcript(context.Background(), "s", "")
			if err != nil || len(transcript.Turns) != 1 || len(transcript.Turns[0].Items) != 2 || transcript.Turns[0].Items[1].Text != "你😀" || transcript.Turns[0].Items[0].Text != "question\n" {
				t.Fatalf("canonical history hid live tail: %+v, %v", transcript, err)
			}
			if transcript.Turns[0].Items[0].TimestampMS == nil || *transcript.Turns[0].Items[0].TimestampMS != 1794877323000 || transcript.Session.UpdatedAt == 0 {
				t.Fatalf("lost serialized timestamps: %+v", transcript)
			}
			epoch, cursor := p.stream().log.Position()
			broadcast(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2,
				"patches":             []any{map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 0, "text"}, "value": "你😀好"}},
				"acceptedTextChanges": []any{map[string]any{"turnId": "t", "itemId": "a"}},
			})
			batch, err := p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			if err != nil || len(batch.Events) != 1 || batch.Events[0].Kind != "text" || batch.Events[0].Offset != 3 || batch.Events[0].Delta != "好" {
				t.Fatalf("Immer live text was lost: %+v %v", batch, err)
			}
			before := string(b.followed["s"].state)
			broadcast(map[string]any{"type": "patches", "baseRevision": 2, "revision": 3, "patches": []any{
				map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 0, "text"}, "value": "must not commit"},
				map[string]any{"op": "remove", "path": []any{"missing", 0}},
			}})
			if f := b.followed["s"]; f.err == nil || f.revision != 2 || string(f.state) != before {
				t.Fatal("invalid patch transaction partially committed")
			}
			// A fresh snapshot must recover the same revision after a rejected patch.
			broadcast(map[string]any{"type": "snapshot", "revision": 2, "conversationState": state})
			if b.followed["s"].err != nil {
				t.Fatal("snapshot did not recover follower")
			}
		})
	}
}

func TestIPCImmerPathsEscapeKeysAndRejectMalformedSegments(t *testing.T) {
	before := json.RawMessage(`{"a/b~c":{"0":["旧"]}}`)
	patch, err := decodeIPCPatches(json.RawMessage(`[{"op":"replace","path":["a/b~c","0",0],"value":"新😀"}]`))
	if err != nil {
		t.Fatal(err)
	}
	after, err := patch.Apply(before)
	if err != nil || !reflect.DeepEqual(decodeObject(after), decodeObject(json.RawMessage(`{"a/b~c":{"0":["新😀"]}}`))) {
		t.Fatalf("escaped key corrupted: %s %v", after, err)
	}
	for _, path := range []string{`null`, `[null]`, `[true]`, `[-1]`, `[0.5]`, `[{}]`} {
		if _, err := decodeIPCPatches(json.RawMessage(`[{"op":"remove","path":` + path + `}]`)); err == nil {
			t.Errorf("accepted malformed Immer path %s", path)
		}
	}
	// Empty array denotes the document root, not a missing path.
	patch, err = decodeIPCPatches(json.RawMessage(`[{"op":"replace","path":[],"value":{"id":"new"}}]`))
	if err != nil {
		t.Fatal(err)
	}
	after, err = patch.Apply(before)
	if err != nil || decodeObject(after)["id"] != "new" {
		t.Fatalf("root replacement failed: %s %v", after, err)
	}
}

func TestIPCFailedFollowerStillReadsHistoryWithoutAcquiringWriter(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	calls := []string{}
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		calls = append(calls, m.Method)
		switch m.Method {
		case "thread/queue/list":
			return map[string]any{"data": []any{}}, nil
		case "thread/read":
			return map[string]any{"thread": map[string]any{"id": "s", "cwd": "/project"}}, nil
		case "thread/turns/list":
			return map[string]any{"data": []any{map[string]any{"id": "t", "status": "completed", "items": []any{map[string]any{"id": "a", "type": "agentMessage", "text": "persisted reply"}}}}}, nil
		default:
			t.Errorf("history acquired writer: %s", m.Method)
			return nil, nil
		}
	})
	f := attachIPCFixture(t, p, nil)
	f.b.mu.Lock()
	f.b.followed["s"] = &followedThread{owner: "app", err: errors.New("unsupported schema"), changed: make(chan struct{})}
	f.b.mu.Unlock()
	out, err := p.Transcript(context.Background(), "s", "")
	if err != nil || len(out.Turns) != 1 || out.Turns[0].Items[0].Text != "persisted reply" {
		t.Fatalf("persisted history hidden by live failure: %+v %v", out, err)
	}
	if !reflect.DeepEqual(calls, []string{"thread/read", "thread/turns/list", "thread/queue/list"}) {
		t.Fatal(calls)
	}
	// App-server pagination remains readable while the IPC follower is broken.
	if _, err := p.Transcript(context.Background(), "s", "older-page"); err != nil {
		t.Fatalf("read-only history pagination failed: %v", err)
	}
	// An IPC pagination cursor must never be forwarded to app-server APIs.
	if _, err := p.Transcript(context.Background(), "s", "ipc:dA"); err == nil {
		t.Fatal("expired IPC cursor accepted")
	}
	if len(calls) != 5 {
		t.Fatal("IPC cursor leaked to app-server", calls)
	}
}
