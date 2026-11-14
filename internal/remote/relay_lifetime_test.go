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

func TestHostRelayLossCannotLeaveOnlineSignal(t *testing.T) {
	token := strings.Repeat("r", 32)
	server := httptest.NewServer(relay.New(token))
	defer server.Close()
	pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	status := NewHostStatus(pair.Relay)
	done := make(chan error, 1)
	go func() {
		done <- serveHostObserved(ctx, pair, Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"test": &testProvider{}}}, status)
	}()
	waitFor(t, func() bool { return status.Snapshot().State == "connected" })
	setup, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	c, err := Connect(setup, pair, Options{RelayOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	status.mu.Lock()
	w := status.peers[c.wire.peer]
	status.mu.Unlock()
	// Lose only the host's data socket. Keeping its signal online would let
	// new peers handshake successfully and then hang during verification.
	_ = w.dataWS.CloseNow()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("host left signaling online with a dead relay response channel")
	}
}
