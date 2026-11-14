package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

type waitingProvider struct {
	*testProvider
	started chan context.Context
	release chan struct{}
}

func (p *waitingProvider) Transcript(ctx context.Context, _, _ string) (provider.Transcript, error) {
	p.started <- ctx
	select {
	case <-p.release:
		return provider.Transcript{Session: provider.Session{ID: "slow-session"}}, nil
	case <-ctx.Done():
		return provider.Transcript{}, ctx.Err()
	}
}

func TestBusinessRequestFollowsConnectionLifetime(t *testing.T) {
	token := strings.Repeat("r", 32)
	server := httptest.NewServer(relay.New(token))
	defer server.Close()
	pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	p := &waitingProvider{testProvider: &testProvider{}, started: make(chan context.Context, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor := NewHostStatus(pair.Relay)
	hostDone := make(chan error, 1)
	go func() {
		hostDone <- serveHostObserved(ctx, pair, Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"test": p}}, monitor)
	}()
	waitFor(t, func() bool { return monitor.Snapshot().State == "connected" })
	setup, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	c, err := Connect(setup, pair, Options{RelayOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	request := control.Request{Method: "transcript", Provider: "test", Session: "slow-session"}
	result := make(chan error, 1)
	go func() { result <- c.Call(ctx, request, nil) }()
	var requestCtx context.Context
	select {
	case requestCtx = <-p.started:
	case <-time.After(time.Second):
		t.Fatal("request not dispatched")
	}
	if deadline, ok := requestCtx.Deadline(); ok {
		t.Fatalf("business request has total deadline: %v", deadline)
	}
	close(p.release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	// A stalled agent must stop when its controller actually disconnects.
	p.release = make(chan struct{})
	go func() { result <- c.Call(ctx, request, nil) }()
	select {
	case requestCtx = <-p.started:
	case <-time.After(time.Second):
		t.Fatal("second request not dispatched")
	}
	_ = c.wire.dataWS.CloseNow()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("lost transport returned success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disconnection left request pending")
	}
	select {
	case <-requestCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("disconnection did not cancel host work")
	}
	cancel()
	select {
	case <-hostDone:
	case <-time.After(time.Second):
		t.Fatal("host leaked")
	}
}

func TestFragmentLifetimeTracksProgress(t *testing.T) {
	secret := strings.Repeat("ab", 32)
	sender, _ := newCryptor(secret)
	receiver, _ := newCryptor(secret)
	var result string
	w := &wire{crypto: receiver, peer: "peer", chunks: map[string]*assembly{}, onData: func(b []byte) { result = string(b) }}
	for i, data := range []string{"large ", "slow ", "snapshot"} {
		if i > 0 {
			// Simulate a minute between parts without sleeping. Each part
			// must renew the inactivity window, unlike the old 30s total age.
			w.chunks["snapshot"].updated = time.Now().Add(-time.Minute)
		}
		plain, _ := json.Marshal(chunk{ID: "snapshot", Index: i, Total: 3, Data: []byte(data)})
		sealed, err := sender.seal(w.aad("chunk", false), plain)
		if err != nil {
			t.Fatal(err)
		}
		w.onChunk(sealed)
		if i < 2 && time.Since(w.chunks["snapshot"].updated) > time.Second {
			t.Fatal("new data did not renew fragment lifetime")
		}
	}
	if result != "large slow snapshot" || w.chunkBytes != 0 {
		t.Fatalf("incomplete slow transfer: %q (%d retained bytes)", result, w.chunkBytes)
	}
}
