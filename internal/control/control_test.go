package control

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
)

type snapshotProvider struct {
	provider.Provider
	title  string
	state  string
	writes atomic.Int32
}

func (p *snapshotProvider) Status(context.Context) provider.Status {
	state := p.state
	if state == "" {
		state = "ready"
	}
	return provider.Status{ID: "test", State: state, Installed: true, Running: true, Detail: "runtime detail"}
}
func (p *snapshotProvider) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{{ID: "same-thread", Title: p.title, Preview: strings.Repeat("history", 300000)}}}, nil
}
func (p *snapshotProvider) Send(context.Context, string, string) error { p.writes.Add(1); return nil }
func TestSnapshotsAvoidRepeatedHistoryAndMutationsExecute(t *testing.T) {
	p := &snapshotProvider{title: "before"}
	d := &Dispatcher{Providers: map[string]provider.Provider{"test": p}}
	r := Request{ID: "first", Method: "sessions", Provider: "test"}
	first := d.Handle(context.Background(), r)
	if first.Error != "" || first.Revision == "" || first.NotModified || len(first.Result) < 1<<20 {
		t.Fatal("bad initial snapshot")
	}
	r.ID = "second"
	r.Revision = first.Revision
	unchanged := d.Handle(context.Background(), r)
	encoded, _ := json.Marshal(unchanged)
	if !unchanged.NotModified || unchanged.Revision != first.Revision || len(unchanged.Result) != 0 || len(encoded) > 200 {
		t.Fatalf("unchanged snapshot was retransmitted (%d bytes)", len(encoded))
	}
	p.title = "after"
	changed := d.Handle(context.Background(), r)
	if changed.NotModified || changed.Revision == first.Revision || !strings.Contains(string(changed.Result), "after") {
		t.Fatal("changed data was incorrectly cached")
	}
	r.Method = "send"
	r.Session = "same-thread"
	r.Text = "continue"
	for i := 0; i < 2; i++ {
		result := d.Handle(context.Background(), r)
		if result.Error != "" || result.NotModified || result.Revision != "" {
			t.Fatal("mutation returned a conditional response")
		}
	}
	if p.writes.Load() != 2 {
		t.Fatal("mutation was cached")
	}
}

func TestControlDependsOnLiveReadinessWithoutEnableSwitch(t *testing.T) {
	p := &snapshotProvider{state: "detected"}
	d := &Dispatcher{Providers: map[string]provider.Provider{"test": p}}
	ctx := context.Background()
	status := func() provider.Status {
		t.Helper()
		response := d.Handle(ctx, Request{Method: "providers"})
		var statuses []provider.Status
		if err := json.Unmarshal(response.Result, &statuses); err != nil {
			t.Fatal(err)
		}
		return statuses[0]
	}
	if s := status(); s.Controllable || !s.Running || s.Detail != "runtime detail" {
		t.Fatalf("bad detected status: %+v", s)
	}
	if r := d.HandleRemote(ctx, Request{Method: "send", Provider: "test", Text: "hello"}); r.Error == "" {
		t.Fatal("unattached provider accepted write")
	}
	p.state = "ready"
	if !status().Controllable {
		t.Fatal("connected provider requires an extra switch")
	}
	if r := d.HandleRemote(ctx, Request{Method: "send", Provider: "test", Text: "hello"}); r.Error != "" {
		t.Fatal(r.Error)
	}
	if p.writes.Load() != 1 {
		t.Fatal("unexpected write count")
	}
}

type probingProvider struct {
	provider.Provider
	log *provider.EventLog
}

func (p *probingProvider) Probe(ctx context.Context, session, epoch string, after int64) (provider.StreamBatch, error) {
	return p.log.Probe(ctx, session, epoch, after)
}
func (p *probingProvider) Watch(ctx context.Context, session, epoch string, after int64) (provider.StreamBatch, error) {
	return p.log.Watch(ctx, session, epoch, after)
}
func TestWatchProbeDispatchesWithoutWaitingForNewText(t *testing.T) {
	p := &probingProvider{log: provider.NewEventLog()}
	epoch, cursor := p.log.Position()
	d := &Dispatcher{Providers: map[string]provider.Provider{"test": p}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A caught-up ordinary Watch would wait; a probe must return immediately.
	result := make(chan Response, 1)
	go func() {
		result <- d.Handle(ctx, Request{Method: "watch", Provider: "test", Session: "chat", Epoch: epoch, After: cursor, Probe: true})
	}()
	select {
	case response := <-result:
		var batch provider.StreamBatch
		if response.Error != "" || json.Unmarshal(response.Result, &batch) != nil || batch.Reset || batch.Epoch != epoch {
			t.Fatal("invalid probe reply", response)
		}
	case <-time.After(time.Second):
		t.Fatal("probe dispatched as a long poll")
	}
}

type taskHistoryFixture struct{ snapshotProvider }

func (*taskHistoryFixture) Transcript(context.Context, string, string) (provider.Transcript, error) {
	return provider.Transcript{Turns: []provider.Turn{{ID: "task", Status: "completed", Items: []provider.Item{{Text: strings.Repeat("private body", 200000)}}}}}, nil
}

type taskStatusFixture struct{ taskHistoryFixture }

func (*taskStatusFixture) TurnStatus(context.Context, string, string) (provider.Turn, error) {
	return provider.Turn{ID: "task", Status: "completed", Items: []provider.Item{{Text: "must not transfer"}}}, nil
}
func TestBackgroundTurnStatusTransfersNoConversationContent(t *testing.T) {
	for _, p := range []provider.Provider{&taskHistoryFixture{}, &taskStatusFixture{}} {
		d := &Dispatcher{Providers: map[string]provider.Provider{"test": p}, Execute: func(Request, func() Response) Response { t.Fatal("status read treated as mutation"); return Response{} }}
		r := d.HandleRemote(context.Background(), Request{Method: "turn.status", Provider: "test", Session: "session", Turn: "task"})
		var turn provider.Turn
		if r.Error != "" || json.Unmarshal(r.Result, &turn) != nil || turn.ID != "task" || turn.Status != "completed" || len(turn.Items) != 0 || len(r.Result) > 100 {
			t.Fatalf("unexpected task state response: %+v", r)
		}
	}
}
