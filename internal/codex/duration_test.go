package codex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestTurnDurationHistoryAndLiveParity(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		want         int64
	}{
		{"recorded", `"durationMs":135123,"startedAt":1700000000,"completedAt":1700000200`, 135123},
		{"timestamp fallback", `"startedAt":1700000000,"completedAt":1700000060`, 60000},
		{"zero", `"durationMs":0`, 0},
		{"unknown", `"durationMs":null`, -1},
		{"negative", `"durationMs":-5`, -1},
		{"reversed", `"startedAt":1700000060,"completedAt":1700000000`, -1},
		{"missing end", `"startedAt":1700000000`, -1},
		{"overflow", `"startedAt":1,"completedAt":9223372036854775807`, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"id":"t","status":"completed",` + tc.fields + `}`)
			check := func(got *int64) {
				t.Helper()
				if tc.want < 0 {
					if got != nil {
						t.Fatalf("invented duration: %d", *got)
					}
				} else if got == nil || *got != tc.want {
					t.Fatalf("duration = %v, want %d", got, tc.want)
				}
			}
			check(normalizeTurn(raw).DurationMS)
			p := &Provider{}
			epoch, cursor := p.stream().log.Position()
			p.observeStream(rpc.Message{Method: "turn/completed", Params: rawJSON(map[string]any{"threadId": "s", "turn": raw})})
			batch, err := p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			if err != nil || len(batch.Events) != 1 {
				t.Fatalf("missing terminal event: %+v %v", batch, err)
			}
			check(batch.Events[0].DurationMS)
		})
	}
}

func TestCanonicalIPCHistoryKeepsRecordedDuration(t *testing.T) {
	state := map[string]any{"turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{
		"isComplete":    true,
		"entitiesByKey": map[string]any{"turn:t": map[string]any{"turnId": "t", "status": "completed", "durationMs": 135123, "items": []any{}}},
		"islands":       []any{map[string]any{"entries": []any{map[string]any{"value": "turn:t"}}}},
	}}}
	turns, _, err := stateTurns(state)
	if err != nil || len(turns) != 1 {
		t.Fatalf("missing canonical turn: %v", err)
	}
	duration := normalizeTurn(rawJSON(turns[0])).DurationMS
	if duration == nil || *duration != 135123 {
		t.Fatal("canonical history lost recorded duration")
	}
}
