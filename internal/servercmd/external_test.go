package servercmd

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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
		node := readTestConfig(t, `{"stun":{"listen":"127.0.0.1:0"},"relay":{"native":{"listen":"127.0.0.1:0"}}}`)
		rt, err := Start(node)
		if err != nil {
			t.Fatal(err)
		}
		defer rt.Close()
		credential, err := deviceservice.ReadRelayCredential(node.State)
		if err != nil {
			t.Fatal(err)
		}
		control := readTestConfig(t, `{"control":{"listen":"127.0.0.1:0","externalRelays":[{"addresses":["`+rt.native.Addr().String()+`"],"credential":"`+credential+`"}],"stunServers":["udp:udp.example:3478","tcp:tcp.example:3478"]},"stun":{"listen":"127.0.0.1:0"}}`)
		ctrl, err := Start(control)
		if err != nil {
			t.Fatal(err)
		}
		defer ctrl.Close()
		origin = "http://" + ctrl.HTTPAddr.String()
		policy := device.Network(t.Context(), origin)
		if len(policy.SelfSTUN) != 2 || !slices.Contains(policy.SelfSTUN, "stun:udp.example:3478") ||
			len(policy.TCPSTUN) != 2 || !slices.Contains(policy.TCPSTUN, "tcp.example:3478") {
			t.Fatal("missing local or external discovery endpoints", policy.SelfSTUN, policy.TCPSTUN)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
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
	welcome := func(role, token string) (*websocket.Conn, relay.Frame) {
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
		return ws, frame
	}
	hostWS, h := welcome("host", host.Token)
	clientWS, c := welcome("client", client.Token)
	if h.Native.Endpoint != c.Native.Endpoint || !strings.HasPrefix(c.Native.Ticket, "rh2.") || c.Native.Ticket == h.Native.Ticket {
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
	// An invalid leg is rejected before a partner joins.
	wrong := join("TCP", "host", "invalid")
	if _, err := wrong.Setup(ctx); err == nil || ctx.Err() != nil {
		t.Fatal("bad ticket failed to get a prompt rejection", err)
	}
	wrong.Close()
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
	// Renew both legs on their existing signal and data sockets. The new
	// authorization must carry traffic past the original 60-second expiry.
	start := time.Now()
	time.Sleep(8 * time.Second)
	renew := func(ws *websocket.Conn, peer string, leg *nativepath.Link) {
		t.Helper()
		if err := ws.Write(ctx, websocket.MessageText, []byte(`{"kind":"renew"}`)); err != nil {
			t.Fatal(err)
		}
		for {
			_, raw, err := ws.Read(ctx)
			var frame relay.Frame
			if err != nil || json.Unmarshal(raw, &frame) != nil {
				t.Fatal("read renewal", err)
			}
			if frame.Kind == "lease" && frame.Peer == peer && frame.Native != nil {
				if err := leg.Send(ctx, nativepath.LeaseLane, []byte(frame.Native.Ticket)); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
	}
	renew(hostWS, h.Peer, a)
	renew(clientWS, c.Peer, b)
	time.Sleep(time.Until(start.Add(61 * time.Second)))
	transfer(a, b, "renewed authorization")
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
}
