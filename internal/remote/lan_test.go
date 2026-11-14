package remote

import (
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/device"
)

func TestRelaySupportsHTTPAndHTTPS(t *testing.T) {
	previous := device.AllowLANHTTP
	t.Cleanup(func() { device.AllowLANHTTP = previous })
	p := Pairing{Host: strings.Repeat("h", 32), Key: strings.Repeat("ab", 32), RelayToken: "test-token"}
	for _, enabled := range []string{"false", "true"} {
		device.AllowLANHTTP = enabled
		for _, tc := range []struct {
			url string
			ok  bool
		}{
			{"ws://127.0.0.1:7443", true},
			{"ws://10.0.4.191:7443", true},
			{"ws://example.com:7443", true},
			{"ws://8.8.8.8:7443", true},
			{"wss://example.com", true},
		} {
			p.Relay = tc.url
			if err := p.Validate(); (err == nil) != tc.ok {
				t.Errorf("LAN=%s URL=%s: err=%v, want accepted=%v", enabled, tc.url, err, tc.ok)
			}
		}
	}
}
