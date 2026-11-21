package browserclient

import (
	"slices"
	"testing"
)

func TestLANHTTPPolicy(t *testing.T) {
	previous := AllowLANHTTP
	t.Cleanup(func() { AllowLANHTTP = previous })
	for _, enabled := range []string{"false", "true"} {
		AllowLANHTTP = enabled
		for _, tc := range []struct {
			host       string
			local, lan bool
		}{
			{"localhost", true, false}, {"127.0.0.1", true, false}, {"::1", true, false},
			{"10.0.4.140", false, true}, {"192.168.1.2", false, true}, {"172.16.0.1", false, true},
			{"8.8.8.8", false, false}, {"172.32.0.1", false, false}, {"ras.lihuipeng.love", false, false},
			{"10.0.4.140.example.com", false, false}, {"fc00::1", false, false}, {"", false, false},
		} {
			want := tc.host != ""
			if got := CleartextAllowed(tc.host); got != want {
				t.Errorf("LAN=%s host=%q got %v want %v", enabled, tc.host, got, want)
			}
		}
	}
}

func TestSTUNPolicyExplicitEmpty(t *testing.T) {
	if len(STUN(nil)) == 0 {
		t.Fatal("missing service configuration lost its fallback")
	}
	if got := STUN(Object{"publicStun": []any{}, "selfStun": []any{}, "tcpStun": []any{"probe.example:26331"}}); len(got) != 0 {
		t.Fatal("browser added UDP probes to a TCP-only configuration", got)
	}
	if got := STUN(Object{"publicStun": []any{}, "selfStun": []any{"stun:probe.example:26331"}}); !slices.Equal(got, []string{"stun:probe.example:26331"}) {
		t.Fatal("browser changed the configured probe list", got)
	}
}
