package relay

import (
	"testing"

	"github.com/monkeyWie/reignhub/internal/nativepath"
)

func TestRelayTransportAddresses(t *testing.T) {
	const shared = "relay.example:26332"
	const tcp = "[2001:db8::1]:443"
	pin := nativepath.Pin(nativepath.Key([]byte("test"), "relay"))
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
			nodes, err := ResolveNodes([]NodeConfig{{Addresses: tc.entries, Pin: pin}})
			if err != nil {
				t.Fatal(err)
			}
			got := nodes[0]
			if got.Pin != pin || got.AddressFor("QUIC") != tc.quic || got.AddressFor("TCP") != tc.tcp {
				t.Fatalf("incorrect routes: %#v", got)
			}
			if tc.name == "shared" && got.Address != shared {
				t.Fatal("lost shared-address compatibility")
			}
		})
	}
	for _, entries := range [][]string{nil, {"tcp:" + tcp, shared}, {"udp://relay.example:443"}, {"udp:[invalid]:443"}, {"tcp:relay.example:0"}} {
		if _, err := ResolveNodes([]NodeConfig{{Addresses: entries, Pin: pin}}); err == nil {
			t.Errorf("accepted invalid relay addresses %v", entries)
		}
	}
}
