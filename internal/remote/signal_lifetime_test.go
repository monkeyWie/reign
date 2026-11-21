package remote

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

func TestDirectRPCSurvivesEitherSignalSocketClosing(t *testing.T) {
	for _, tcp := range []bool{false, true} {
		name := "WebRTC"
		if tcp {
			name = "TCP"
		}
		t.Run(name, func(t *testing.T) {
			token := strings.Repeat("r", 32)
			server := httptest.NewServer(relay.New(token))
			defer server.Close()
			pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			status := NewHostStatus(pair.Relay)
			fake := &testProvider{log: provider.NewEventLog()}
			opts := Options{DisableNative: true, DisableTCP: !tcp, DisableWebRTC: tcp}
			done := make(chan error, 1)
			go func() {
				done <- serveHostObserved(ctx, pair, opts, &control.Dispatcher{Providers: map[string]provider.Provider{"test": fake}}, status)
			}()
			waitFor(t, func() bool { return status.Snapshot().State == "connected" })
			setup, stop := context.WithTimeout(ctx, 10*time.Second)
			defer stop()
			c, err := Connect(setup, pair, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			waitFor(t, func() bool { return strings.HasPrefix(c.Path(), "P2P") })
			status.mu.Lock()
			host := status.peers[c.wire.peer]
			status.mu.Unlock()
			if host == nil {
				t.Fatal("authenticated host missing")
			}
			_ = c.wire.ws.CloseNow()
			waitFor(t, func() bool { return c.wire.signalLost.Load() && host.signalLost.Load() })
			if !strings.HasPrefix(c.Path(), "P2P") {
				t.Fatal("client signal loss destroyed direct path", c.Path())
			}
			if err = c.Call(setup, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil {
				t.Fatal("RPC after client signal loss", err)
			}
			_ = host.ws.CloseNow()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("host signal did not detach")
			}
			// Simulate the host reconnect loop updating its public service status.
			status.set("connecting", "信令正在恢复")
			if len(status.Snapshot().Peers) != 1 {
				t.Fatal("healthy direct peer disappeared during signal recovery")
			}
			if err = c.Call(setup, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil {
				t.Fatal("RPC after host signal loss", err)
			}
			if fake.sends.Load() != 2 {
				t.Fatal("direct requests were lost or duplicated")
			}
			if tcp {
				c.wire.tcpSnapshot().close()
			} else {
				c.wire.mu.Lock()
				pc := c.wire.pc
				c.wire.mu.Unlock()
				_ = pc.Close()
			}
			waitFor(t, func() bool { return c.Path() == "Disconnected" })
		})
	}
}
