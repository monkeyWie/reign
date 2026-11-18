package codex

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestIPCRecencyUsesSubmissionAndCompletionOnly(t *testing.T) {
	const start int64 = 1794960000123
	for _, canonical := range []bool{false, true} {
		turn := map[string]any{"turnId": "t", "status": "inProgress", "turnStartedAtMs": start, "items": []any{}}
		state := map[string]any{"id": "s", "updatedAt": start + 999999}
		if canonical {
			state["turnHistory"] = map[string]any{"kind": "canonical", "history": map[string]any{"entitiesByKey": map[string]any{"t": turn}, "islands": []any{map[string]any{"entries": []any{map[string]any{"key": "t"}}}}}}
		} else {
			state["turns"] = []any{turn}
		}
		check := func(want int64) {
			t.Helper()
			ss := provider.Session{ID: "s"}
			applyIPCActivity(&ss, rawJSON(state))
			if ss.RecencyAt != want {
				t.Fatalf("canonical=%v: recency=%d, want %d", canonical, ss.RecencyAt, want)
			}
		}
		check(start)
		turn["items"] = []any{map[string]any{"id": "a", "type": "agentMessage", "phase": "commentary", "text": "working", "startedAtMs": start + 2000}}
		turn["durationMs"] = int64(3000) // A live duration is not completion.
		check(start)
		turn["items"] = append(turn["items"].([]any), map[string]any{"id": "u", "type": "steeringUserMessage", "input": []any{map[string]any{"type": "text", "text": "follow-up"}}, "restoreMessage": map[string]any{"createdAt": start + 4000}})
		check(start + 4000)
		turn["status"], turn["durationMs"] = "completed", int64(6000)
		check(start + 6000)
		state["updatedAt"] = start + 9999999
		check(start + 6000)
		turn["status"] = "interrupted"
		check(start + 4000)
	}
}

func TestDirectoryRecencyMetadataCachingAndRecovery(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	var calls atomic.Int32
	var fail atomic.Bool
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		if m.Method != "thread/turns/list" {
			t.Errorf("unexpected read %s", m.Method)
		}
		params := decodeObject(m.Params)
		if params["itemsView"] != "notLoaded" || params["limit"] != float64(1) {
			t.Errorf("directory loaded conversation contents: %s", m.Params)
		}
		calls.Add(1)
		if fail.Load() {
			return nil, &rpc.Error{Code: -32000, Message: "fixture unavailable"}
		}
		return json.RawMessage(`{"data":[{"id":"t","status":"completed","startedAt":1794960000,"completedAt":1794960060}]}`), nil
	})
	read := func(updated, want int64) {
		t.Helper()
		page := []provider.Session{{ID: "s", Status: "notLoaded", UpdatedAt: updated}}
		p.enrichSessionRecency(context.Background(), page)
		if page[0].RecencyAt != want {
			t.Fatalf("recency=%d, want %d", page[0].RecencyAt, want)
		}
	}
	const completed int64 = 1794960060000
	read(completed, completed)
	read(completed, completed)
	if calls.Load() != 1 {
		t.Fatal("unchanged directory repeated metadata reads")
	}
	// A metadata edit/late snapshot must not change the last conversation event.
	read(completed+1000, completed)
	fail.Store(true)
	read(completed+2000, completed)
	fail.Store(false)
	read(completed+2000, completed)
	if calls.Load() != 4 {
		t.Fatal("failed read was cached as a successful reconciliation")
	}
	p.rememberSessionRecency("s", completed+3000)
	read(completed, completed+3000)
}
