package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/relay"
)

func dial(ctx context.Context, p Pairing, role string) (*websocket.Conn, *websocket.Conn, string, error) {
	return dialWithTimeout(ctx, p, role, connectivity.StageTimeout)
}
func dialWithTimeout(ctx context.Context, p Pairing, role string, timeout time.Duration) (*websocket.Conn, *websocket.Conn, string, error) {
	ws, data, welcome, err := dialService(ctx, p, role, timeout, Options{RelayOnly: true, DisableNative: true})
	return ws, data, welcome.Peer, err
}

func delayedWelcomeServer(t *testing.T, signalDelay, relayDelay time.Duration) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		delay := signalDelay
		if r.URL.Path == "/p2p/relay" {
			delay = relayDelay
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(delay):
		}
		raw, _ := json.Marshal(relay.Frame{Kind: "welcome", Protocol: 1, Peer: "peer", AllowRelay: true})
		if ws.Write(r.Context(), websocket.MessageText, raw) != nil {
			return
		}
		_, _, _ = ws.Read(r.Context())
	}))
	t.Cleanup(server.Close)
	return server
}
func timeoutPair(server *httptest.Server) Pairing {
	return Pairing{Relay: strings.Replace(server.URL, "http", "ws", 1), Host: strings.Repeat("h", 16), Key: strings.Repeat("ab", 32), RelayToken: "test"}
}
func TestSignalAndRelayHaveIndependentTimeouts(t *testing.T) {
	server := delayedWelcomeServer(t, 650*time.Millisecond, 650*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, data, _, err := dialWithTimeout(ctx, timeoutPair(server), "client", time.Second)
	if err != nil {
		t.Fatalf("each stage fits its own budget, but combined exceeds it: %v", err)
	}
	defer ws.CloseNow()
	defer data.CloseNow()
}
func TestRelayTimeoutIdentifiesStageAndCancellationStillWorks(t *testing.T) {
	// Leave time for the signal handshake under race/coverage instrumentation;
	// only the deliberately stalled relay should exhaust a stage's budget.
	server := delayedWelcomeServer(t, 0, 2*time.Second)
	_, _, _, err := dialWithTimeout(context.Background(), timeoutPair(server), "client", time.Second)
	if err == nil || !strings.Contains(err.Error(), "中继") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost relay timeout detail: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err = dial(ctx, timeoutPair(server), "client")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}
