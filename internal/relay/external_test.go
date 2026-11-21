package relay

import (
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/nativepath"
)

func TestRelayTransportAddresses(t *testing.T) {
	const shared = "relay.example:26332"
	const tcp = "[2001:db8::1]:443"
	key := nativepath.Key([]byte("test"), "relay")
	pin := nativepath.Pin(key)
	credential := Credential(key)
	for _, tc := range []struct {
		name      string
		entries   []string
		quic, tcp string
	}{
		{"shared", []string{shared}, shared, shared},
		{"split", []string{"tcp:" + tcp, "udp:" + shared}, shared, tcp},
		{"tcp only", []string{"tcp:" + tcp}, "", tcp},
		{"udp only", []string{"udp:" + shared}, shared, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := ResolveNodes([]NodeConfig{{Addresses: tc.entries, Credential: credential}})
			if err != nil {
				t.Fatal(err)
			}
			got := nodes[0]
			if got.Pin != pin || got.AddressFor("QUIC") != tc.quic || got.AddressFor("TCP") != tc.tcp {
				t.Fatalf("incorrect routes: %#v", got.Endpoint)
			}
			if tc.name == "shared" && got.Address != shared {
				t.Fatal("lost shared-address compatibility")
			}
		})
	}
	for _, entries := range [][]string{nil, {"tcp:" + tcp, shared}, {"udp://relay.example:443"}, {"udp:[invalid]:443"}, {"tcp:relay.example:0"}} {
		if _, err := ResolveNodes([]NodeConfig{{Addresses: entries, Credential: credential}}); err == nil {
			t.Errorf("accepted invalid relay addresses %v", entries)
		}
	}
}

func TestExternalTicketScope(t *testing.T) {
	key := nativepath.Key([]byte("test"), "first")
	nodes, err := ResolveNodes([]NodeConfig{{Addresses: []string{"relay.example:26332"}, Credential: Credential(key)}})
	if err != nil {
		t.Fatal(err)
	}
	host, gen, peer := ID(), ID(), ID()
	now := time.Unix(1_800_000_000, 0)
	until := now.Add(time.Minute)
	ticket := nodes[0].issue(host, gen, peer, "client", until).Ticket
	valid := NativeJoin{Host: host, Peer: peer, Role: "client", Ticket: ticket}
	if got, expiry, ok := verifyTicket(authorizationKey(key), valid, now); !ok || got != gen || !expiry.Equal(until) {
		t.Fatal("valid ticket lost its generation or expiry")
	}
	for _, tc := range []struct {
		name string
		key  []byte
		join NativeJoin
		now  time.Time
	}{
		{"wrong node", authorizationKey(nativepath.Key([]byte("test"), "second")), valid, now},
		{"wrong host", nodes[0].secret, NativeJoin{ID(), peer, "client", ticket}, now},
		{"wrong peer", nodes[0].secret, NativeJoin{host, ID(), "client", ticket}, now},
		{"wrong role", nodes[0].secret, NativeJoin{host, peer, "host", ticket}, now},
		{"expired", nodes[0].secret, valid, until},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := verifyTicket(tc.key, tc.join, tc.now); ok {
				t.Fatal("out-of-scope credential accepted")
			}
		})
	}
}
