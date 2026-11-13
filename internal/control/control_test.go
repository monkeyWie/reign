package control

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monkeyWie/reign/internal/provider"
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
