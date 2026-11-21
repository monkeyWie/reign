package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/codexipc"
)

func ipcProjectionFixture(turnCount, itemCount int) map[string]any {
	turns := []any{}
	for i := range turnCount {
		items := []any{}
		for j := range itemCount {
			items = append(items, map[string]any{"id": fmt.Sprint(j), "type": "commandExecution", "status": "completed", "command": "inspect fixture", "aggregatedOutput": strings.Repeat("synthetic output\n", 32)})
		}
		turns = append(turns, map[string]any{"id": fmt.Sprint(i), "status": "completed", "items": items})
	}
	turns = append(turns, map[string]any{"id": "live", "status": "inProgress", "items": []any{map[string]any{"id": "reply", "type": "agentMessage", "text": "你😀"}}})
	return map[string]any{"id": "fixture", "turns": turns}
}

func TestIPCProjectionKeepsHistoricalCorrectionsAndLiveDelta(t *testing.T) {
	p := &Provider{pending: map[string]pending{}}
	state := ipcProjectionFixture(4, 3)
	old := rawJSON(state)
	turns := array(state["turns"])
	object(array(object(turns[0])["items"])[0])["command"] = "corrected command"
	object(array(object(turns[4])["items"])[0])["text"] = "你😀好"
	next := rawJSON(state)
	epoch, cursor := p.stream().log.Position()
	observeFixtureState(t, p, old, next)
	batch, err := p.stream().log.Probe(context.Background(), "fixture", epoch, cursor)
	if err != nil || len(batch.Events) != 2 {
		t.Fatalf("expected a historical correction and live delta: %+v %v", batch, err)
	}
	correction, delta := batch.Events[0], batch.Events[1]
	if correction.Kind != "item" || correction.TurnID != "0" || correction.Item == nil || correction.Item.Activities[0].Detail != "corrected command" {
		t.Fatal("skipped a changed historical turn", correction)
	}
	if delta.Kind != "text" || delta.TurnID != "live" || delta.Offset != 3 || delta.Delta != "好" {
		t.Fatal("live UTF-16 delta changed", delta)
	}
	_, cursor = p.stream().log.Position()
	observeFixtureState(t, p, next, next)
	batch, err = p.stream().log.Probe(context.Background(), "fixture", epoch, cursor)
	if err != nil || len(batch.Events) != 0 {
		t.Fatal("unchanged history produced events", batch, err)
	}
	// Projection owns its top-level maps/slices, without rewriting vendor state.
	projected, _, err := stateTurns(state)
	if err != nil {
		t.Fatal(err)
	}
	projected[0]["status"], projected[0]["items"] = "failed", []any{}
	if string(rawJSON(state)) != string(next) {
		t.Fatal("history projection mutated source state")
	}
}

func BenchmarkIPCObserveHistoryDelta(b *testing.B) {
	state := ipcProjectionFixture(40, 50)
	old := rawJSON(state)
	turns := array(state["turns"])
	object(array(object(turns[len(turns)-1])["items"])[0])["text"] = "你😀好"
	next := rawJSON(state)
	p := &Provider{pending: map[string]pending{}}
	b.ReportAllocs()
	b.SetBytes(int64(len(old) + len(next)))
	b.ResetTimer()
	for range b.N {
		observeFixtureState(b, p, old, next)
	}
}

func TestIPCModelOnlyChangeInvalidatesTranscript(t *testing.T) {
	p := &Provider{pending: map[string]pending{}}
	state := ipcProjectionFixture(1, 1)
	state["latestModel"] = "model-a"
	old := rawJSON(state)
	state["latestModel"] = "model-b"
	next := rawJSON(state)
	epoch, cursor := p.stream().log.Position()
	observeFixtureState(t, p, old, next)
	batch, err := p.stream().log.Probe(context.Background(), "fixture", epoch, cursor)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Kind != "snapshot" {
		t.Fatalf("model-only change did not wake the controller: %+v %v", batch, err)
	}
	_, cursor = p.stream().log.Position()
	observeFixtureState(t, p, next, next)
	batch, err = p.stream().log.Probe(context.Background(), "fixture", epoch, cursor)
	if err != nil || len(batch.Events) != 0 {
		t.Fatal("unchanged metadata caused repeated refreshes", batch, err)
	}
}

func BenchmarkIPCBroadcastHistoryDelta(b *testing.B) {
	state := rawJSON(ipcProjectionFixture(40, 50))
	p := &Provider{pending: map[string]pending{}}
	bridge := newAppIPC(p)
	defer bridge.cancel()
	f := &followedThread{owner: "owner", version: 13}
	bridge.followed["fixture"] = f
	m := codexipc.Message{Method: "thread-stream-state-changed", Source: f.owner, Version: f.version, Params: rawJSON(map[string]any{"conversationId": "fixture", "hostId": "local", "change": map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{map[string]any{"op": "replace", "path": []any{"turns", 40, "items", 0, "text"}, "value": "你😀好"}}}})}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		f.revision, f.state = 1, state
		bridge.Broadcast(m)
		if f.err != nil || f.revision != 2 {
			b.Fatal("patch failed", f.err)
		}
	}
}

func observeFixtureState(t testing.TB, p *Provider, old, next json.RawMessage) {
	t.Helper()
	state := decodeObject(next)
	p.observeIPCState("fixture", "owner", old, state, fixtureStateTurns(t, state))
}
