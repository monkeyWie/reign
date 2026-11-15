//go:build darwin && !ios

package system

import "testing"

func TestDirectDesktopPeerMatching(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   bool
	}{
		{"p123\nn127.0.0.1:59000->127.0.0.1:4500\n", true},
		{"p123\nn127.0.0.1:4500->127.0.0.1:59000\n", false},
		{"p123\nn127.0.0.1:59000->127.0.0.1:45001\n", false},
		{"p123\nn127.0.0.1:59000->10.0.0.1:4500\n", false},
		{"", false},
	} {
		if got := connectedToPeer(tc.output, "127.0.0.1:4500"); got != tc.want {
			t.Errorf("%q: got %v", tc.output, got)
		}
	}
}
