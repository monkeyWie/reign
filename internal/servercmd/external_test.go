package servercmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/monkeyWie/reignhub/internal/relay"
)

// Default to an isolated Go control and standalone Go data plane. The same wire
// checks can target a locally configured Worker and external Go node.
func TestExternalNativeRelay(t *testing.T) {
	origin := os.Getenv("REIGNHUB_TEST_EXTERNAL_CONTROL")
	if origin == "" {
		server := httptest.NewUnstartedServer(nil)
		origin = "http://" + server.Listener.Addr().String()
		node := testConfig(t)
		node.HTTP, node.STUN, node.TCPSTUN = false, false, false
		node.RelayControl = origin
		rt, err := Start(node)
		if err != nil {
			t.Fatal(err)
		}
		defer rt.Close()
		pin, err := deviceservice.ReadRelayPin(node.State)
		if err != nil {
			t.Fatal(err)
		}
		control := testConfig(t)
		control.NativeListen = ""
		control.ExternalRelays = []relay.NodeConfig{{Addresses: []string{rt.native.Addr().String()}, Pin: pin}}
		control.ExternalSTUN = connectivity.TransportAddresses{"udp:udp.example:3478", "tcp:tcp.example:3478"}
		ctrl, err := Start(control)
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != relay.NativeLeasePath {
				ctrl.http.Handler.ServeHTTP(w, r)
				return
			}
			// Shorten successful leases in this fixture to exercise revocation
			// of an active data path without waiting a minute in normal CI.
			rec := httptest.NewRecorder()
			ctrl.http.Handler.ServeHTTP(rec, r)
			w.WriteHeader(rec.Code)
			if rec.Code == http.StatusOK {
				_, _ = io.WriteString(w, `{"validFor":2}`)
			}
		})
		server.Start()
		defer server.Close()
		policy := device.Network(t.Context(), origin)
		if len(policy.SelfSTUN) != 2 || !slices.Contains(policy.SelfSTUN, "stun:udp.example:3478") ||
			len(policy.TCPSTUN) != 2 || !slices.Contains(policy.TCPSTUN, "tcp.example:3478") {
			t.Fatal("missing local or external discovery endpoints", policy.SelfSTUN, policy.TCPSTUN)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Second)
	defer cancel()
	register := func(isHost bool) device.Identity {
		i, err := device.NewIdentity("external-relay-fixture")
		if err != nil {
			t.Fatal(err)
		}
		i, err = device.Register(ctx, origin, i, isHost)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	host, client := register(true), register(false)
	id := relay.ID()
	post := func(path, token string, data any) {
		t.Helper()
		if err := device.Post(ctx, origin, token, path, data, nil); err != nil {
			t.Fatal(err)
		}
	}
	post("/p2p/pairings/request", client.Token, map[string]any{"host": host.ID, "id": id, "envelope": "fixture"})
	post("/p2p/pairings/respond", host.Token, map[string]any{"id": id, "allow": true, "reply": "fixture"})
	welcome := func(role, token string) relay.Frame {
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(origin, "http")+"/p2p/signal?role="+role+"&id="+host.ID, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ws.CloseNow() })
		_, raw, err := ws.Read(ctx)
		var frame relay.Frame
		if err != nil || json.Unmarshal(raw, &frame) != nil || frame.Native == nil {
			t.Fatal("missing native welcome", err)
		}
		return frame
	}
	h, c := welcome("host", host.Token), welcome("client", client.Token)
	if h.Native.Endpoint != c.Native.Endpoint || !strings.HasPrefix(c.Native.Ticket, "ext1.") || c.Native.Ticket == h.Native.Ticket {
		t.Fatal("incorrect external assignment")
	}
	join := func(network, role, ticket string) *nativepath.Link {
		l, err := nativepath.Dial(ctx, c.Native.Endpoint, network)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(l.Close)
		raw, _ := json.Marshal(relay.NativeJoin{Host: host.ID, Peer: c.Peer, Role: role, Ticket: ticket})
		if err = l.Send(ctx, 0, raw); err != nil {
			t.Fatal(err)
		}
		return l
	}
	// A matched pair with a valid client but another host ticket is denied.
	wrong := join("QUIC", "host", "ext1."+relay.ID()+relay.ID())
	probe := join("TCP", "client", c.Native.Ticket)
	if _, err := probe.Setup(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("bad ticket failed to get a prompt rejection", err)
	}
	wrong.Close()
	probe.Close()
	a, b := join("QUIC", "host", h.Native.Ticket), join("TCP", "client", c.Native.Ticket)
	for _, leg := range []*nativepath.Link{a, b} {
		if ack, err := leg.Setup(ctx); err != nil || string(ack) != "ready" {
			t.Fatal("relay admission", err)
		}
	}
	transfer := func(sender, receiver *nativepath.Link, value string) {
		if err := sender.Send(ctx, 0, []byte(value)); err != nil {
			t.Fatal(err)
		}
		got, err := receiver.Setup(ctx)
		if err != nil || string(got) != value {
			t.Fatal("external transfer", err)
		}
	}
	transfer(a, b, "opaque host record")
	transfer(b, a, "opaque controller record")
	post("/p2p/pairings/revoke", host.Token, map[string]any{"client": client.ID})
	// Keep both native sockets open. The node must stop forwarding even
	// if a revoked controller ignores the signaling disconnect.
	expiry, stop := context.WithTimeout(ctx, 62*time.Second)
	defer stop()
	if _, err := b.Setup(expiry); err == nil || expiry.Err() != nil {
		t.Fatal("active relay outlived its authorization", err)
	}
	a.Close()
	b.Close()
	a, b = join("TCP", "host", h.Native.Ticket), join("TCP", "client", c.Native.Ticket)
	if _, err := b.Setup(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("revoked signal tickets still usable", err)
	}
}
