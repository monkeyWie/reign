package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

type blockedStatusProvider struct {
	*testProvider
	checks atomic.Int32
}

func (p *blockedStatusProvider) Status(ctx context.Context) provider.Status {
	p.checks.Add(1)
	<-ctx.Done()
	return provider.Status{ID: "blocked", State: "error"}
}

func TestDirectConnectDoesNotWaitForRelayOrAgentDiscovery(t *testing.T) {
	for _, tcp := range []bool{true, false} {
		name := "WebRTC"
		if tcp {
			name = "TCP"
		}
		t.Run(name, func(t *testing.T) {
			token := strings.Repeat("r", 32)
			room := relay.New(token)
			releaseRelay := make(chan struct{})
			var relayRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/p2p/relay" {
					relayRequests.Add(1)
					select {
					case <-releaseRelay:
					case <-r.Context().Done():
						return
					}
				}
				room.ServeHTTP(w, r)
			}))
			defer server.Close()
			pair := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			monitor := NewHostStatus(pair.Relay)
			p := &blockedStatusProvider{testProvider: &testProvider{}}
			opts := Options{DisableWebRTC: tcp, DisableTCP: !tcp}
			go serveHostObserved(ctx, pair, opts, &control.Dispatcher{Providers: map[string]provider.Provider{"blocked": p}}, monitor)
			waitFor(t, func() bool { return monitor.Snapshot().State == "connected" })
			// Optional services remain blocked until after Connect returns.
			// Allow race-instrumented CI enough scheduling time for direct ICE/TCP.
			setup, stop := context.WithTimeout(ctx, 30*time.Second)
			started := time.Now()
			c, err := Connect(setup, pair, opts)
			stop()
			if err != nil {
				t.Fatal("direct connection waited on a stalled optional service:", err)
			}
			defer c.Close()
			t.Logf("authenticated %s in %s with relay blocked and agent discovery stalled", name, time.Since(started).Round(time.Millisecond))
			if p.checks.Load() != 0 || !strings.HasPrefix(c.Path(), "P2P") || c.wire.relaySocket() != nil {
				t.Fatal("connection readiness depended on agent discovery or pending relay", c.Path())
			}
			waitFor(t, func() bool { return relayRequests.Load() == 2 })
			// Completing the connection RPC must not cancel its pending backup.
			close(releaseRelay)
			waitFor(t, func() bool { return c.wire.relaySocket() != nil })
			monitor.mu.Lock()
			host := monitor.peers[c.wire.peer]
			monitor.mu.Unlock()
			waitFor(t, func() bool { return host.relaySocket() != nil })
			for _, w := range []*wire{c.wire, host} {
				if tcp {
					w.closeTCP()
				} else {
					w.mu.Lock()
					pc := w.pc
					w.mu.Unlock()
					_ = pc.Close()
				}
			}
			waitFor(t, func() bool { return strings.HasPrefix(c.Path(), "Relay") && strings.HasPrefix(host.path(), "Relay") })
			probe, stopProbe := context.WithTimeout(ctx, 3*time.Second)
			defer stopProbe()
			if err := c.Call(probe, control.Request{Method: "network"}, nil); err != nil {
				t.Fatal("background relay did not remain usable after connection RPC ended:", err)
			}
		})
	}
}
