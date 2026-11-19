package control

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

type questionProvider struct {
	snapshotProvider
	decisions int
}

func (p *questionProvider) Approvals() []provider.Approval {
	return []provider.Approval{{ID: "a", SessionID: "first", Questions: []provider.Question{{ID: "q", Question: "Scope?"}}}, {ID: "b", SessionID: "second"}}
}
func (p *questionProvider) Decide(context.Context, string, provider.Decision) error {
	p.decisions++
	return nil
}

func TestQuestionsAreScopedToSessionOnLocalAndRemoteRoutes(t *testing.T) {
	p := &questionProvider{}
	d := &Dispatcher{Providers: map[string]provider.Provider{"test": p}}
	for _, handle := range []func(context.Context, Request) Response{d.Handle, d.HandleRemote} {
		result := handle(context.Background(), Request{Method: "approvals", Provider: "test", Session: "first"})
		var approvals []provider.Approval
		if json.Unmarshal(result.Result, &approvals) != nil || len(approvals) != 1 || len(approvals[0].Questions) != 1 {
			t.Fatalf("questions lost session scope: %+v", result)
		}
		bad := handle(context.Background(), Request{Method: "decide", Provider: "test", Session: "second", Approval: "a"})
		if bad.Error == "" || p.decisions != 0 {
			t.Fatal("cross-session answer reached Provider")
		}
	}
	good := d.Handle(context.Background(), Request{Method: "decide", Provider: "test", Session: "first", Approval: "a"})
	if good.Error != "" || p.decisions != 1 {
		t.Fatalf("correctly scoped answer failed: %+v", good)
	}
}
