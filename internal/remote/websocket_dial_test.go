package remote

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
)

func TestRelayUpgradeRecoversFromOneAbortedConnection(t *testing.T) {
	var signals, relays atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/p2p/relay" {
			if relays.Add(1) == 1 {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					conn.Close()
				}
				return
			}
		} else {
			signals.Add(1)
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		if ws.Write(r.Context(), websocket.MessageText, []byte(`{"kind":"welcome","peer":"peer","protocol":1,"allowRelay":true}`)) != nil {
			return
		}
		_, _, _ = ws.Read(r.Context())
	}))
	defer server.Close()
	var retry bool
	ctx := connectivity.WithProgress(context.Background(), func(p connectivity.Progress) {
		if strings.Contains(p.ID, "transport-retry") {
			retry = true
		}
	})
	ws, data, _, err := dialWithTimeout(ctx, timeoutPair(server), "client", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	defer data.CloseNow()
	if signals.Load() != 1 || relays.Load() != 2 || !retry {
		t.Fatalf("unexpected full reconnect/retry: signals=%d relays=%d progress=%v", signals.Load(), relays.Load(), retry)
	}
}
func TestWebSocketRejectionKeepsHTTPDetailAndDoesNotRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("CF-Ray", "diagnostic-ray")
		w.WriteHeader(409)
		io.WriteString(w, `{"code":"SIGNAL_REQUIRED","message":"请先建立信令连接"}`)
	}))
	defer server.Close()
	_, response, err := dialWebSocket(context.Background(), server.URL, "secret", "relay")
	failure := websocketDialFailure("建立备用中继通道", response, err)
	for _, s := range []string{"HTTP 409", "SIGNAL_REQUIRED", "请先建立信令连接", "diagnostic-ray"} {
		if !strings.Contains(failure.Error(), s) {
			t.Fatalf("missing %s: %v", s, failure)
		}
	}
	if requests.Load() != 1 || strings.Contains(failure.Error(), "secret") {
		t.Fatal("retried rejection or exposed token")
	}
}
func TestTransportRetryIsBoundedByStageDeadline(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := dialWebSocket(ctx, server.URL, "secret", "relay")
	if !errors.Is(err, context.DeadlineExceeded) || requests.Load() != 1 {
		t.Fatalf("retry ignored stage budget: requests=%d err=%v", requests.Load(), err)
	}
}
func TestTransportRetriesStopAfterThreeAttempts(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	defer server.Close()
	_, _, err := dialWebSocket(context.Background(), server.URL, "secret", "relay")
	if err == nil || requests.Load() != 3 {
		t.Fatalf("unbounded or missing retries: requests=%d err=%v", requests.Load(), err)
	}
}

func TestStalledUpgradeGetsFreshSocketWithoutClosingSuccessfulConnection(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		// Send after the setup budget has expired to prove it does not become a
		// lifetime deadline on the upgraded socket.
		time.Sleep(150 * time.Millisecond)
		_ = ws.Write(r.Context(), websocket.MessageText, []byte("still-connected"))
		_, _, _ = ws.Read(r.Context())
	}))
	defer server.Close()
	ctx, stop := context.WithTimeout(context.Background(), 4*time.Second)
	defer stop()
	ws, _, err := dialWebSocketBudget(ctx, server.URL, "secret", "signal", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	_, data, err := ws.Read(ctx)
	if err != nil || string(data) != "still-connected" || requests.Load() != 2 {
		t.Fatalf("fresh-socket fallback failed: %q %d %v", data, requests.Load(), err)
	}
}
