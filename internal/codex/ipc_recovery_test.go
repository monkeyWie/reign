package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func recoveryState(status string) map[string]any {
	items := []any{map[string]any{"id": "reply", "type": "agentMessage", "phase": "commentary", "text": "Working"}}
	if status == "completed" {
		items = []any{
			map[string]any{"id": "server-user", "type": "userMessage", "clientId": "sent", "content": []any{}},
			map[string]any{"id": "steer", "type": "steeringUserMessage", "clientUserMessageId": "steered", "status": "accepted", "input": []any{}},
			map[string]any{"id": "pending", "type": "steeringUserMessage", "clientUserMessageId": "pending", "status": "pending", "input": []any{}},
			map[string]any{"id": "reply", "type": "agentMessage", "phase": "final_answer", "text": "Final answer"},
		}
	}
	return map[string]any{"id": "selected", "turns": []any{map[string]any{"turnId": "t", "status": status, "items": items}}}
}

func recoverySnapshot(version int, revision int64, state map[string]any) codexipc.Message {
	return codexipc.Message{Type: "broadcast", Method: "thread-stream-state-changed", Version: version, Source: "app-owner", Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "selected", "change": map[string]any{"type": "snapshot", "revision": revision, "conversationState": state}})}
}

func seedRecoveryFollower(f *ipcFixture, version int) {
	f.b.mu.Lock()
	f.b.followed["selected"] = &followedThread{owner: "app-owner", changed: make(chan struct{})}
	f.b.mu.Unlock()
	f.b.Broadcast(recoverySnapshot(version, 1, recoveryState("inProgress")))
}

func invalidateRecoveryFollower(b *appIPC) {
	b.mu.Lock()
	b.followed["selected"].refreshGeneration++
	b.mu.Unlock()
}

func recoveryMetadataPatch(version int, revision int64) codexipc.Message {
	return codexipc.Message{Type: "broadcast", Method: "thread-stream-state-changed", Version: version, Source: "app-owner", Params: rawJSON(map[string]any{
		"hostId": "local", "conversationId": "selected", "change": map[string]any{
			"type": "patches", "baseRevision": revision - 1, "revision": revision,
			"patches": []any{map[string]any{"op": "add", "path": []string{"title"}, "value": "Metadata updated"}},
		},
	})}
}

func TestIPCOwnerAnnouncementSurvivesMetadataTraffic(t *testing.T) {
	for _, version := range []int{11, 13} {
		for _, read := range []string{"watch", "history"} {
			t.Run(fmt.Sprintf("v%d/%s", version, read), func(t *testing.T) {
				p := New(config.Config{}, "", events.New())
				backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
					if m.Method != "thread/queue/list" {
						t.Errorf("unexpected private executor request: %s", m.Method)
					}
					return map[string]any{"data": []any{}}, nil
				})
				var follows atomic.Int32
				var f *ipcFixture
				f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
					if m.Method == "thread-stream-following-changed" {
						follows.Add(1)
						go f.send(recoverySnapshot(version, 3, recoveryState("completed")))
					} else if m.Method != "thread-owner-discovery" {
						t.Errorf("recovery issued a command: %s", m.Method)
					}
					return codexipc.Message{}, false
				})
				seedRecoveryFollower(f, version)
				f.b.Broadcast(codexipc.Message{Method: "thread-stream-following-status-requested", Version: 1, Source: "app-owner", Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "selected"})})
				// Metadata arriving after an owner announcement cannot erase
				// the pending subscription invalidation.
				f.b.Broadcast(recoveryMetadataPatch(version, 2))
				epoch, cursor := p.stream().log.Position()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if read == "watch" {
					batch, err := p.Probe(ctx, "selected", epoch, cursor)
					if err != nil || batch.Reset || len(batch.Events) == 0 {
						t.Fatalf("healthy cursor hid missed completion: %+v %v", batch, err)
					}
				}
				out, err := p.Transcript(ctx, "selected", "")
				if err != nil || len(out.Turns) != 1 || out.Turns[0].Status != "completed" || follows.Load() != 1 {
					t.Fatalf("metadata kept stale running state fresh: %+v %v follows=%d", out, err, follows.Load())
				}
			})
		}
	}
}

func TestIPCInvalidatedFollowerRecoversCompletionAndConsumedQueue(t *testing.T) {
	for _, version := range []int{11, 13} {
		for _, read := range []string{"watch", "history"} {
			t.Run(fmt.Sprintf("v%d/%s", version, read), func(t *testing.T) {
				p := New(config.Config{}, "", events.New())
				backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
					if m.Method != "thread/queue/list" {
						t.Errorf("unexpected private executor request: %s", m.Method)
					}
					return map[string]any{"data": []any{map[string]any{"id": "server-queue-id", "clientUserMessageId": "sent", "input": []any{}}}}, nil
				})
				var follows atomic.Int32
				var f *ipcFixture
				f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
					if m.Method == "thread-stream-following-changed" {
						follows.Add(1)
						go f.send(recoverySnapshot(version, 2, recoveryState("completed")))
					} else if m.Method != "thread-owner-discovery" {
						t.Errorf("recovery issued a command: %s", m.Method)
					}
					return codexipc.Message{}, false
				})
				seedRecoveryFollower(f, version)
				messages := []any{}
				for _, id := range []string{"sent", "steered", "pending", "same-text"} {
					messages = append(messages, map[string]any{"id": id, "text": "same text"})
				}
				writeQueueJSON(t, f.b.home, messages)
				f.b.Broadcast(codexipc.Message{Method: "thread-queued-followups-changed", Version: 3, Source: "app-owner", Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "selected", "messages": messages})})
				epoch, cursor := p.stream().log.Position()
				invalidateRecoveryFollower(f.b)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if read == "watch" {
					batch, err := p.Watch(ctx, "selected", epoch, cursor)
					if err != nil || batch.Reset || len(batch.Events) == 0 {
						t.Fatalf("valid cursor hid missed completion: %+v %v", batch, err)
					}
				}
				out, err := p.Transcript(ctx, "selected", "")
				if err != nil || len(out.Turns) != 1 || out.Turns[0].Status != "completed" || out.Turns[0].Items[len(out.Turns[0].Items)-1].Text != "Final answer" {
					t.Fatalf("stale running answer: %+v %v", out, err)
				}
				if out.Queue == nil || out.Queue.State != "ready" || len(out.Queue.Messages) != 2 || out.Queue.Messages[0].ID != "pending" || out.Queue.Messages[1].ID != "same-text" {
					t.Fatalf("consumed queue or optimistic/equal-text confusion: %+v", out.Queue)
				}
				// A late/stale queue broadcast must not reintroduce accepted IDs.
				f.b.Broadcast(codexipc.Message{Method: "thread-queued-followups-changed", Version: 3, Source: "app-owner", Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "selected", "messages": messages})})
				out, err = p.Transcript(ctx, "selected", "")
				if err != nil || len(out.Queue.Messages) != 2 || follows.Load() != 1 {
					t.Fatalf("healthy reads repeated follow or resurrected queue: %+v %v follows=%d", out.Queue, err, follows.Load())
				}
			})
		}
	}
}

func TestIPCFreshSnapshotBarrierAndCancellation(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	requested := make(chan struct{}, 8)
	f := attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-stream-following-changed" {
			requested <- struct{}{}
		}
		return codexipc.Message{}, false
	})
	seedRecoveryFollower(f, 13)
	invalidateRecoveryFollower(f.b)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.b.refreshFollower(ctx, "selected") }()
	select {
	case <-requested:
	case <-time.After(time.Second):
		t.Fatal("refresh never requested")
	}
	select {
	case err := <-done:
		t.Fatalf("cached state satisfied fresh barrier: %v", err)
	default:
	}
	// A joining reader can cancel independently; it must not cancel the refresh.
	joining, stop := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer stop()
	if err := f.b.refreshFollower(joining, "selected"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("joining cancellation", err)
	}
	if len(requested) != 0 {
		t.Fatal("concurrent refresh duplicated subscription")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("refresh cancellation", err)
	}
	// A fresh attempt must succeed even if the owner's revision is unchanged.
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { done <- f.b.refreshFollower(ctx, "selected") }()
	<-requested
	f.send(recoverySnapshot(13, 1, recoveryState("inProgress")))
	if err := <-done; err != nil {
		t.Fatal("unchanged owner snapshot failed to acknowledge recovery", err)
	}
}

func TestIPCRefreshFailureDoesNotPresentCachedStateAsFresh(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			p := New(config.Config{}, "", events.New())
			backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
				switch m.Method {
				case "thread/read":
					return map[string]any{"thread": map[string]any{"id": "selected"}}, nil
				case "thread/turns/list":
					return map[string]any{"data": []any{map[string]any{"id": "t", "status": "completed", "items": []any{map[string]any{"id": "server-user", "type": "userMessage", "clientId": "sent", "content": []any{}}, map[string]any{"id": "reply", "type": "agentMessage", "text": "Persisted final"}}}}}, nil
				case "thread/queue/list":
					return map[string]any{"data": []any{}}, nil
				default:
					t.Errorf("read acquired writer: %s", m.Method)
					return nil, nil
				}
			})
			f := attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
				err := "discovery-failed"
				if unavailable {
					err = "no-client-found"
				}
				return codexipc.Message{ResultType: "error", Error: err}, true
			})
			seedRecoveryFollower(f, 11)
			writeQueueJSON(t, f.b.home, []any{map[string]any{"id": "sent", "text": "already sent"}})
			invalidateRecoveryFollower(f.b)
			var previews provider.TranscriptBuffer
			ctx := provider.WithTranscriptObserver(context.Background(), previews.Add)
			out, err := p.Transcript(ctx, "selected", "")
			if err != nil || out.Turns[0].Status != "completed" || strings.Contains(string(rawJSON(previews.Transcript)), "Working") {
				t.Fatalf("failed refresh published stale IPC preview: %+v %v", out, err)
			}
			if out.Queue.State != "ready" || len(out.Queue.Messages) != 0 {
				t.Fatal("persisted delivery receipt failed to reconcile stale queue", out.Queue)
			}
		})
	}
}

func TestIPCRefreshTimeoutBackoffAndRecovery(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	var requests atomic.Int32
	var recover atomic.Bool
	var f *ipcFixture
	f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		requests.Add(1)
		if m.Method == "thread-stream-following-changed" && recover.Load() {
			go f.send(recoverySnapshot(13, 2, recoveryState("completed")))
		}
		return codexipc.Message{}, false
	})
	seedRecoveryFollower(f, 13)
	invalidateRecoveryFollower(f.b)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := f.b.refreshFollower(ctx, "selected"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("silent owner did not respect read budget", err)
	}
	count := requests.Load()
	// A valid metadata patch cannot erase failed-refresh cooldown or make
	// the stale conversation eligible for the cached-success path again.
	f.b.Broadcast(recoveryMetadataPatch(13, 2))
	if err := f.b.refreshFollower(context.Background(), "selected"); !errors.Is(err, context.DeadlineExceeded) || requests.Load() != count {
		t.Fatal("failed refresh spun instead of backing off", err)
	}
	f.b.mu.Lock()
	f.b.followed["selected"].refreshAfter = time.Time{}
	f.b.mu.Unlock()
	recover.Store(true)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.b.refreshFollower(ctx, "selected"); err != nil {
		t.Fatal("fresh attempt did not recover", err)
	}
}

func TestIPCRefreshRejectsOwnerLoss(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	requested := make(chan struct{}, 1)
	f := attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
		if m.Method == "thread-stream-following-changed" {
			requested <- struct{}{}
		}
		return codexipc.Message{}, false
	})
	seedRecoveryFollower(f, 13)
	invalidateRecoveryFollower(f.b)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.b.refreshFollower(ctx, "selected") }()
	<-requested
	f.b.Broadcast(codexipc.Message{Method: "client-status-changed", Params: rawJSON(map[string]any{"clientId": "app-owner", "status": "disconnected"})})
	f.b.Broadcast(recoverySnapshot(13, 2, recoveryState("completed")))
	if err := <-done; err == nil {
		t.Fatal("departed owner's snapshot satisfied recovery")
	}
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	if len(f.b.followed["selected"].state) != 0 || len(f.b.owned) != 0 {
		t.Fatal("owner loss restored stale state or acquired writer")
	}
}
