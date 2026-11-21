package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
)

func TestNativeDataPaths(t *testing.T) {
	for _, tc := range []struct {
		name, clientPath, hostPath string
		direct, hostTCP, clientTCP bool
	}{
		{"direct QUIC", "P2P · QUIC", "P2P · QUIC", true, false, false},
		{"relay QUIC", "Relay · QUIC", "Relay · QUIC", false, false, false},
		{"relay TCP", "Relay · TCP", "Relay · TCP", false, true, true},
		{"mixed relay", "Relay · TCP", "Relay · QUIC", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := strings.Repeat("r", 32)
			room := relay.New(token)
			defer room.Close()
			// Exercise native-only signaling as well as a failed WebSocket fallback.
			room.DisableWebSocket = tc.name == "mixed relay"
			if !tc.direct {
				listener, err := room.ListenNative("127.0.0.1:0", "", nativepath.Key([]byte("fixture"), "server"))
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			} else {
				room.RelayAllowed = func(string, string, string) bool { return false }
			}
			// Reject all WS data sockets to verify both legs of the actual RPC.
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/p2p/relay" {
					http.Error(w, "disabled", 503)
					return
				}
				room.ServeHTTP(w, r)
			}))
			defer server.Close()
			p := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
			fake := &testProvider{log: provider.NewEventLog()}
			status := NewHostStatus(p.Relay)
			d := &control.Dispatcher{Providers: map[string]provider.Provider{"test": fake}, Network: status.Snapshot}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			go RunHostObserved(ctx, p, Options{RelayOnly: !tc.direct, DisableTCP: true, DisableWebRTC: true, DisableQUIC: tc.hostTCP}, d, status)
			waitFor(t, func() bool { return status.Snapshot().State == "connected" })
			client, err := Connect(ctx, p, Options{RelayOnly: !tc.direct, DisableTCP: true, DisableWebRTC: true, DisableQUIC: tc.clientTCP})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if client.Path() != tc.clientPath {
				t.Fatal(client.Path())
			}
			var netState control.NetworkStatus
			if err := client.Call(ctx, control.Request{Method: "network"}, &netState); err != nil || len(netState.Peers) != 1 || netState.Peers[0].Path != tc.hostPath {
				t.Fatal(netState, err)
			}
			var sessions provider.SessionPage
			if err := client.Call(ctx, control.Request{Method: "sessions", Provider: "test"}, &sessions); err != nil || len(sessions.Sessions) != 1 {
				t.Fatal(err)
			}
			if err := client.Call(ctx, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil || fake.sends.Load() != 1 {
				t.Fatal(fake.sends.Load(), err)
			}
			if tc.name == "relay QUIC" {
				client.wire.native.relay.Load().Close()
				waitFor(t, func() bool { return client.Path() == "Relay · TCP" })
				if err := client.Call(ctx, control.Request{Method: "network"}, &netState); err != nil || netState.Peers[0].Path != "Relay · TCP" || fake.sends.Load() != 1 {
					t.Fatal("TCP recovery", netState, err)
				}
				room.DisconnectDevice(p.Host, "")
				waitFor(t, func() bool { return client.Path() == "Disconnected" })
			}
		})
	}
}

func TestNativeAdmissionIsBoundToSignalPeer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token := strings.Repeat("x", 32)
	room := relay.New(token)
	defer room.Close()
	li, err := room.ListenNative("127.0.0.1:0", "", nativepath.Key([]byte("admission"), "relay"))
	if err != nil {
		t.Fatal(err)
	}
	defer li.Close()
	server := httptest.NewServer(room)
	defer server.Close()
	p := Pairing{Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token, Relay: "ws" + strings.TrimPrefix(server.URL, "http")}
	host, _, err := dialSignal(ctx, p, "host", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer host.CloseNow()
	a, first, err := dialSignal(ctx, p, "client", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CloseNow()
	b, second, err := dialSignal(ctx, p, "client", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer b.CloseNow()
	for _, network := range []string{"QUIC", "TCP"} {
		wrong := first.Native.Endpoint
		wrong.Pin = nativepath.Pin(nativepath.Key([]byte("unrelated"), "relay"))
		if link, err := nativepath.Dial(ctx, wrong, network); err == nil {
			link.Close()
			t.Fatal("accepted wrong relay key", network)
		}
		link, err := nativepath.Dial(ctx, first.Native.Endpoint, network)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := json.Marshal(relay.NativeJoin{Host: p.Host, Peer: second.Peer, Role: "client", Ticket: first.Native.Ticket})
		if err = link.Send(ctx, 0, req); err != nil {
			t.Fatal(err)
		}
		_, err = link.Setup(ctx)
		link.Close()
		if err == nil || ctx.Err() != nil {
			t.Fatalf("%s did not immediately reject another signal peer's ticket: %v", network, err)
		}
	}
}
