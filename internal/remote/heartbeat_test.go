package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
)

func TestApplicationHeartbeatKeepsSignalAndRelayAliveWithoutControlPongs(t *testing.T) {
	for _, renew := range []bool{false, true} {
		for _, reply := range []string{"reign:pong", `{"kind":"lease"}`} {
			name := "relay"
			if renew {
				name = "signal"
			}
			t.Run(name+"/"+reply, func(t *testing.T) {
				var pings, renewals, controlPings atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OnPingReceived: func(context.Context, []byte) bool { controlPings.Add(1); return false }})
					if err != nil {
						return
					}
					defer ws.CloseNow()
					for {
						_, raw, err := ws.Read(r.Context())
						if err != nil {
							return
						}
						if string(raw) == "reign:ping" {
							pings.Add(1)
							if ws.Write(r.Context(), websocket.MessageText, []byte(reply)) != nil {
								return
							}
						} else {
							var message struct {
								Kind string `json:"kind"`
							}
							if json.Unmarshal(raw, &message) == nil && message.Kind == "renew" {
								renewals.Add(1)
							}
						}
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				ws, _, err := websocket.Dial(ctx, server.URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer ws.CloseNow()
				heartbeat := newSocketHeartbeat()
				reading := make(chan error, 1)
				go func() { reading <- drainRelay(ctx, ws, heartbeat) }()
				stopped := make(chan struct{})
				go func() {
					defer close(stopped)
					heartbeat.runWithPolicy(ctx, ws, renew, nil, 20*time.Millisecond, 120*time.Millisecond, time.Second)
				}()
				deadline := time.NewTimer(3 * time.Second)
				defer deadline.Stop()
				poll := time.NewTicker(10 * time.Millisecond)
				defer poll.Stop()
				for pings.Load() < 10 || renew && renewals.Load() < 3 {
					select {
					case err := <-reading:
						t.Fatalf("healthy application heartbeat closed the %s: %v", name, err)
					case <-deadline.C:
						t.Fatal("heartbeat/renewal did not continue across the inactivity budget")
					case <-poll.C:
					}
				}
				if controlPings.Load() != 0 || !renew && renewals.Load() != 0 {
					t.Fatal("wrong heartbeat or relay tried renewing the signal lease")
				}
				cancel()
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("heartbeat leaked")
				}
				select {
				case <-reading:
				case <-time.After(time.Second):
					t.Fatal("reader leaked")
				}
			})
		}
	}
}

func TestApplicationHeartbeatReportsSilentSocketBeforeClosing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		// A half-open path can accept writes without returning any application data.
		for {
			if _, _, err := ws.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws, _, err := websocket.Dial(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	heartbeat := newSocketHeartbeat()
	reading := make(chan error, 1)
	go func() { reading <- drainRelay(ctx, ws, heartbeat) }()
	warning := make(chan connectivity.Progress, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		heartbeat.runWithPolicy(ctx, ws, true, func(p connectivity.Progress) { warning <- p }, 20*time.Millisecond, 120*time.Millisecond, time.Second)
	}()
	select {
	case p := <-warning:
		if p.ID != "signal-heartbeat" || p.State != "warning" || !strings.Contains(p.Detail, "未收到服务消息或心跳响应") {
			t.Fatal("missing failure cause", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("silent socket was left connected forever")
	}
	select {
	case <-reading:
	case <-time.After(time.Second):
		t.Fatal("silent socket was not closed")
	}
	<-stopped
}
