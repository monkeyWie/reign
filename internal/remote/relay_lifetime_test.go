package remote

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

func TestBrowserHeartbeatOnSignalAndRelay(t *testing.T) {
	token := strings.Repeat("r", 32)
	server := httptest.NewServer(relay.New(token))
	defer server.Close()
	pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	signal, data, _, err := dial(ctx, pair, "host")
	if err != nil {
		t.Fatal(err)
	}
	defer signal.CloseNow()
	defer data.CloseNow()
	for _, socket := range []*websocket.Conn{signal, data} {
		if err := socket.Write(ctx, websocket.MessageText, []byte("reign:ping")); err != nil {
			t.Fatal(err)
		}
		_, raw, err := socket.Read(ctx)
		if err != nil || string(raw) != "reign:pong" {
			t.Fatalf("heartbeat: %q, %v", raw, err)
		}
	}
}

func TestHostRepairsReplyRelayWithoutDisconnectingPeers(t *testing.T) {
	for _, relayOnly := range []bool{true, false} {
		name := "P2P"
		if relayOnly {
			name = "Relay"
		}
		t.Run(name, func(t *testing.T) {
			token := strings.Repeat("r", 32)
			server := httptest.NewServer(relay.New(token))
			defer server.Close()
			pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			status := NewHostStatus(pair.Relay)
			done := make(chan error, 1)
			opts := Options{RelayOnly: relayOnly, DisableTCP: true}
			go func() {
				done <- serveHostObserved(ctx, pair, opts, &control.Dispatcher{Providers: map[string]provider.Provider{"test": &testProvider{}}}, status)
			}()
			waitFor(t, func() bool { return status.Snapshot().State == "connected" })
			setup, stop := context.WithTimeout(ctx, 10*time.Second)
			defer stop()
			c, err := Connect(setup, pair, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if !relayOnly {
				waitFor(t, func() bool { return strings.HasPrefix(c.Path(), "P2P") })
			}
			status.mu.Lock()
			w := status.peers[c.wire.peer]
			status.mu.Unlock()
			old := w.relaySocket()
			old.CloseNow()
			waitFor(t, func() bool { socket := w.relaySocket(); return socket != nil && socket != old })
			select {
			case err := <-done:
				t.Fatalf("reply relay loss closed signal: %v", err)
			default:
			}
			if !relayOnly && !strings.HasPrefix(c.Path(), "P2P") {
				t.Fatal("backup loss destroyed the direct path")
			}
			var result any
			if err = c.Call(setup, control.Request{Method: "providers"}, &result); err != nil {
				t.Fatal("existing authenticated peer failed after repair:", err)
			}
		})
	}
}
