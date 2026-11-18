package remote

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

type progressiveProvider struct {
	*testProvider
	release chan struct{}
}

func (p *progressiveProvider) Transcript(ctx context.Context, _, _ string) (provider.Transcript, error) {
	out := provider.Transcript{StreamEpoch: "history", StreamCursor: 7, Session: provider.Session{ID: "session"}, NextCursor: "older"}
	for i := range 40 {
		turn := provider.Turn{ID: fmt.Sprint(i), Status: "completed", DurationMS: new(int64(135000 + i)), Items: []provider.Item{{ID: "message", Role: "assistant", Text: fmt.Sprintf("消息 %d🙂", i)}}}
		out.Turns = append(out.Turns, turn)
		if err := provider.EmitTranscriptTurn(ctx, out, turn); err != nil {
			return provider.Transcript{}, err
		}
		if i == 0 {
			select {
			case <-p.release:
			case <-ctx.Done():
				return provider.Transcript{}, ctx.Err()
			}
		}
	}
	return out, nil
}

func TestTranscriptMessagesArriveBeforeSnapshotCompletes(t *testing.T) {
	token := strings.Repeat("r", 32)
	server := httptest.NewServer(relay.New(token))
	defer server.Close()
	pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	p := &progressiveProvider{testProvider: &testProvider{}, release: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	monitor := NewHostStatus(pair.Relay)
	go RunHostObserved(ctx, pair, Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"test": p}}, monitor)
	waitFor(t, func() bool { return monitor.Snapshot().State == "connected" })
	c, err := Connect(ctx, pair, Options{RelayOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fragments := make(chan provider.Transcript, 64)
	requestCtx := provider.WithTranscriptObserver(ctx, func(fragment provider.Transcript) error { fragments <- fragment; return nil })
	var out provider.Transcript
	done := make(chan error, 1)
	go func() {
		done <- c.Call(requestCtx, control.Request{Method: "transcript", Provider: "test", Session: "session", Progress: true}, &out)
	}()
	select {
	case first := <-fragments:
		if first.Turns[0].Items[0].Text != "消息 0🙂" {
			t.Fatal(first)
		}
		if first.Turns[0].DurationMS == nil || *first.Turns[0].DurationMS != 135000 {
			t.Fatal("encrypted history fragment lost duration")
		}
	case <-ctx.Done():
		t.Fatal("first message blocked on complete snapshot")
	}
	select {
	case err := <-done:
		t.Fatal("snapshot finished before provider released", err)
	default:
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 39 || len(out.Turns) != 40 || out.Turns[39].Items[0].Text != "消息 39🙂" || out.NextCursor != "older" || out.StreamCursor != 7 {
		t.Fatalf("history fragments were dropped or reordered: fragments=%d transcript=%+v", len(fragments), out)
	}
	if out.Turns[39].DurationMS == nil || *out.Turns[39].DurationMS != 135039 {
		t.Fatal("assembled remote transcript lost duration")
	}
}

type rapidHistoryProvider struct {
	*progressiveProvider
	sent chan struct{}
}

func (p *rapidHistoryProvider) Transcript(ctx context.Context, session, cursor string) (provider.Transcript, error) {
	out, err := p.progressiveProvider.Transcript(ctx, session, cursor)
	close(p.sent)
	return out, err
}
func TestSlowHistoryConsumerCannotBlockOtherRelayResponses(t *testing.T) {
	token := strings.Repeat("r", 32)
	server := httptest.NewServer(relay.New(token))
	defer server.Close()
	pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	ready := make(chan struct{})
	close(ready)
	p := &rapidHistoryProvider{progressiveProvider: &progressiveProvider{testProvider: &testProvider{}, release: ready}, sent: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	monitor := NewHostStatus(pair.Relay)
	go RunHostObserved(ctx, pair, Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"test": p}}, monitor)
	waitFor(t, func() bool { return monitor.Snapshot().State == "connected" })
	c, err := Connect(ctx, pair, Options{RelayOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	consume := make(chan struct{})
	requestCtx := provider.WithTranscriptObserver(ctx, func(provider.Transcript) error {
		select {
		case <-consume:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- c.Call(requestCtx, control.Request{Method: "transcript", Provider: "test", Session: "session", Progress: true}, nil)
	}()
	select {
	case <-p.sent:
	case <-ctx.Done():
		t.Fatal("host did not send the history")
	}
	probe, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := c.Call(probe, control.Request{Method: "network"}, nil); err != nil {
		t.Fatal("slow history consumer blocked unrelated response", err)
	}
	close(consume)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("history did not finish")
	}
}
