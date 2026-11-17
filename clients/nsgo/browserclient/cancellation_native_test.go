//go:build !js

package browserclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/internal/remote"
	"github.com/monkeyWie/reignhub/internal/transport"
)

type heldRequestPeer struct {
	peer
	write func([]byte) error
}

func (*heldRequestPeer) Direct() bool             { return true }
func (*heldRequestPeer) Close()                   {}
func (p *heldRequestPeer) Send(data []byte) error { return p.write(data) }

func TestCancelledReadWhileWriteCompletesStillReachesHost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		life, stop := context.WithCancel(context.Background())
		defer stop()
		crypto, err := newCryptor(strings.Repeat("ab", 32))
		if err != nil {
			t.Fatal(err)
		}
		c := &Client{ctx: life, cancel: stop, crypto: crypto, pending: map[string]*pendingCall{}, cache: map[string]cached{}, chunks: map[string]*assembly{}}
		defer c.Close()
		requests := make(chan Object, 2)
		release := make(chan struct{})
		defer close(release)
		c.pc = &heldRequestPeer{write: func(packet []byte) error {
			plain, err := crypto.open(c.aad("chunk", true), packet)
			if err != nil {
				return err
			}
			var part chunk
			if err := json.Unmarshal(plain, &part); err != nil {
				return err
			}
			raw, err := transport.Decode(part.Data, part.Encoding, part.RawSize)
			if err != nil {
				return err
			}
			var request Object
			if err := json.Unmarshal(raw, &request); err != nil {
				return err
			}
			requests <- request
			if request["method"] == "transcript" {
				<-release
			}
			return nil
		}}
		read, cancel := context.WithCancel(life)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := c.Call(read, Object{"id": "history", "method": "transcript", "provider": "fixture", "session": "one"})
			result <- err
		}()
		if request := <-requests; request["method"] != "transcript" {
			t.Fatal(request)
		}
		// The request reached the host while its local write is still pending.
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		release <- struct{}{}
		select {
		case request := <-requests:
			if request["method"] != control.CancelRead || request["cancel"] != "history" || request["session"] != "one" {
				t.Fatalf("missing scoped read cancellation: %+v", request)
			}
		case <-time.After(time.Second):
			t.Fatal("request write cancellation left the host reading")
		}
		if life.Err() != nil {
			t.Fatal("cancelling a read closed its connection")
		}
	})
}

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
