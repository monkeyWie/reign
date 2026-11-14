package remote

import (
	"context"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/pion/webrtc/v4"
)

func TestWebRTCStartupFailureReportsActualReason(t *testing.T) {
	var progress []connectivity.Progress
	w, err := newWire(context.Background(), nil, "test-peer", strings.Repeat("ab", 32), false, Options{
		DisableTCP: true,
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:example.com:invalid"}}},
		Progress:   func(p connectivity.Progress) { progress = append(progress, p) },
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	w.offer()
	for _, p := range progress {
		if p.State == "warning" && p.Stage == "udp" && p.Detail != "" {
			return
		}
	}
	t.Fatal("WebRTC startup failure was silently swallowed", progress)
}
