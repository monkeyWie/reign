//go:build !js

package browserclient

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/internal/remote"
)

type cancellableHistory struct {
	*fixtureProvider
	started chan context.Context
}

func (p *cancellableHistory) Transcript(ctx context.Context, _, _ string) (provider.Transcript, error) {
	p.started <- ctx
	<-ctx.Done()
	return provider.Transcript{}, ctx.Err()
}

func TestBrowserCancellationStopsHostHistoryWithoutDisconnecting(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	token := strings.Repeat("r", 32)
	server := httptest.NewServer(relay.New(token))
	defer server.Close()
	pair := remote.Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	fake := &cancellableHistory{fixtureProvider: &fixtureProvider{}, started: make(chan context.Context, 1)}
	monitor := remote.NewHostStatus(pair.Relay)
	go remote.RunHostObserved(ctx, pair, remote.Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake}}, monitor)
	for monitor.Snapshot().State != "connected" {
		select {
		case <-ctx.Done():
			t.Fatal("host did not connect")
		case <-time.After(time.Millisecond):
		}
	}
	c, err := Connect(ctx, Pairing{Relay: pair.Relay, Host: pair.Host, Key: pair.Key, Token: token, RelayOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Call(readCtx, Object{"method": "transcript", "provider": "fixture", "session": "one"})
		result <- err
	}()
	var hostCtx context.Context
	select {
	case hostCtx = <-fake.started:
	case <-ctx.Done():
		t.Fatal("history not dispatched")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled history succeeded")
		}
	case <-ctx.Done():
		t.Fatal("browser cancellation stalled")
	}
	select {
	case <-hostCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("browser did not cancel host history")
	}
	if _, err := c.Call(ctx, Object{"method": "network"}); err != nil {
		t.Fatal("connection lost after cancellation", err)
	}
}
