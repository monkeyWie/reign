package codex

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestIPCOwnerStreamResetImmediatelyRebasesRevision(t *testing.T) {
	for _, version := range []int{11, 13} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			p := New(config.Config{}, "", events.New())
			f := attachIPCFixture(t, p, nil)
			seedRecoveryFollower(f, version)
			f.b.Broadcast(recoverySnapshot(version, 500, recoveryState("inProgress")))
			epoch, cursor := p.stream().log.Position()
			// The App resets stream revisions on backend reconnect while the
			// same IPC client remains alive. Its full snapshot starts a new base.
			f.b.Broadcast(recoverySnapshot(version, 0, recoveryState("inProgress")))
			f.b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Version: version, Source: "app-owner", Params: rawJSON(map[string]any{
				"hostId": "local", "conversationId": "selected", "change": map[string]any{
					"type": "patches", "baseRevision": 0, "revision": 1,
					"patches": []any{map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 0, "text"}, "value": "Working after reset"}},
				},
			})})
			f.b.Broadcast(recoverySnapshot(version, 2, recoveryState("completed")))
			batch, err := p.stream().log.Probe(context.Background(), "selected", epoch, cursor)
			completed, streamed := false, false
			for _, event := range batch.Events {
				completed = completed || event.Kind == "turn" && event.Status == "completed"
				streamed = streamed || event.Kind == "text" && event.Delta == " after reset"
			}
			if err != nil || !streamed || !completed {
				t.Fatalf("live stream froze after revision reset: streamed=%t completed=%t err=%v", streamed, completed, err)
			}
		})
	}
}

func TestIPCNewOwnerAnnouncementRestoresSubscription(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		if m.Method != "thread/queue/list" {
			t.Errorf("handoff reached private writer: %s", m.Method)
		}
		return nil, &rpc.Error{Code: -32601, Message: "Unsupported method"}
	})
	var f *ipcFixture
	f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		switch m.Method {
		case "thread-owner-discovery":
			return codexipc.Message{ResultType: "success", HandledBy: "new-owner", Result: rawJSON(map[string]any{})}, true
		case "thread-stream-following-changed":
			if len(m.Targets) != 1 || m.Targets[0] != "new-owner" {
				t.Error("follow did not use discovered owner")
			}
			go func() {
				snapshot := recoverySnapshot(13, 1, recoveryState("completed"))
				snapshot.Source = "new-owner"
				f.send(snapshot)
			}()
		default:
			t.Errorf("handoff issued unexpected request: %s", m.Method)
		}
		return codexipc.Message{}, false
	})
	seedRecoveryFollower(f, 13)
	epoch, cursor := p.stream().log.Position()
	f.b.Broadcast(codexipc.Message{Method: "thread-stream-following-status-requested", Version: 1, Source: "new-owner", Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "selected"})})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := p.Watch(ctx, "selected", epoch, cursor)
	if err != nil || len(batch.Events) == 0 {
		t.Fatalf("new owner announcement was ignored: %+v %v", batch, err)
	}
	// Owner replacement publishes a snapshot invalidation, which is the normal
	// event-driven history path. No expiry, polling or user navigation is used.
	f.b.Broadcast(recoverySnapshot(13, 501, recoveryState("inProgress")))
	out, ok, err := p.ipcTranscript(ctx, "selected", "")
	if err != nil || !ok || len(out.Turns) != 1 || out.Turns[0].Status != "completed" {
		t.Fatalf("handoff did not replace the old owner's state: %+v %t %v", out, ok, err)
	}
}

func TestIPCRevisionRegressionRequestsFreshBaseline(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	var f *ipcFixture
	f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-stream-following-changed" {
			go f.send(recoverySnapshot(13, 2, recoveryState("completed")))
		} else if m.Method != "thread-owner-discovery" {
			t.Errorf("unexpected recovery method: %s", m.Method)
		}
		return codexipc.Message{}, false
	})
	seedRecoveryFollower(f, 13)
	f.b.Broadcast(recoverySnapshot(13, 500, recoveryState("inProgress")))
	epoch, cursor := p.stream().log.Position()
	// Even if the reset snapshot was lost, the next lower patch is evidence
	// that this baseline cannot be trusted. Do not silently ignore it forever.
	f.b.Broadcast(recoveryMetadataPatch(13, 1))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := p.Watch(ctx, "selected", epoch, cursor)
	if err != nil || len(batch.Events) == 0 {
		t.Fatalf("revision regression did not repair immediately: %+v %v", batch, err)
	}
	turns, _, err := stateTurns(decodeObject(f.b.activityState("selected")))
	if err != nil || len(turns) != 1 || stringValue(turns[0]["status"]) != "completed" {
		t.Fatal("new baseline was not applied", err)
	}
}
