package control

import (
	"context"
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

type removalProvider struct {
	provider.Provider
	mode, session string
	calls         int
}

func (p *removalProvider) Status(context.Context) provider.Status {
	return provider.Status{State: "ready", Capabilities: provider.Capabilities{SessionRemoval: p.mode}}
}
func (p *removalProvider) RemoveSession(_ context.Context, id string) error {
	p.session = id
	p.calls++
	return nil
}

func TestSessionRemovalUsesSharedMutationBoundary(t *testing.T) {
	p := &removalProvider{mode: "archive"}
	receipts := 0
	d := Dispatcher{Providers: map[string]provider.Provider{"test": p}, Execute: func(r Request, call func() Response) Response { receipts++; return call() }}
	for _, remote := range []bool{false, true} {
		r := Request{ID: "remove", Method: "session.remove", Provider: "test", Session: "chosen"}
		var response Response
		if remote {
			response = d.HandleRemote(context.Background(), r)
		} else {
			response = d.HandleRequest(context.Background(), r)
		}
		if response.Error != "" || response.Revision != "" || response.NotModified || p.session != "chosen" {
			t.Fatal(response, p.session)
		}
	}
	if receipts != 2 || p.calls != 2 || ReadOnly("session.remove") || CancellableRead("session.remove") {
		t.Fatal("removal bypassed mutation semantics")
	}
	p.mode = ""
	if r := d.HandleRequest(context.Background(), Request{Method: "session.remove", Provider: "test", Session: "chosen"}); r.Error == "" || p.calls != 2 {
		t.Fatal("unsupported provider accepted removal")
	}
}
