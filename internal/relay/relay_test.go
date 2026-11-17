package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLateReplyToClosedControllerDoesNotDisconnectHost(t *testing.T) {
	for _, route := range []string{"/p2p/signal", "/p2p/relay"} {
		t.Run(route, func(t *testing.T) {
			server := New("token")
			httpServer := httptest.NewServer(server)
			defer httpServer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			host := ID()
			hostPeer := ""
			dial := func(path, role string) *websocket.Conn {
				t.Helper()
				ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+path+"?id="+host+"&role="+role+"&peer="+hostPeer, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token"}}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ws.CloseNow() })
				if _, raw, err := ws.Read(ctx); err != nil {
					t.Fatal(err)
				} else if path == "/p2p/signal" && role == "host" {
					var frame Frame
					_ = json.Unmarshal(raw, &frame)
					hostPeer = frame.Peer
				}
				return ws
			}
			signal := dial("/p2p/signal", "host")
			sender := signal
			if route == "/p2p/relay" {
				sender = dial(route, "host")
			}
			// Reproduce a response already addressed to a controller whose
			// transport has closed, before its route has been cleaned up.
			dead := dial("/p2p/signal", "client")
			dead.CloseNow()
			server.mu.Lock()
			server.rooms[host].clients["stale"] = &connection{ws: dead}
			server.mu.Unlock()
			kind := "signal"
			if route == "/p2p/relay" {
				kind = "data"
			}
			raw, _ := json.Marshal(Frame{Peer: "stale", Kind: kind})
			if err := sender.Write(ctx, websocket.MessageText, raw); err != nil {
				t.Fatal(err)
			}
			// The same host must still accept a new controller and route data.
			client := dial("/p2p/signal", "client")
			raw, _ = json.Marshal(Frame{Kind: "hello", Body: []byte("ready")})
			if err := client.Write(ctx, websocket.MessageText, raw); err != nil {
				t.Fatal(err)
			}
			for {
				_, raw, err := signal.Read(ctx)
				if err != nil {
					t.Fatalf("late reply disconnected shared host: %v", err)
				}
				var frame Frame
				_ = json.Unmarshal(raw, &frame)
				if frame.Kind == "hello" && string(frame.Body) == "ready" {
					break
				}
			}
		})
	}
}

func TestHostReplacementCannotRemoveNewRoomAndRenewAcknowledgesController(t *testing.T) {
	s := New("token")
	defer s.Close()
	s.Grant = func(_, _, _ string) string { return "signed-test-lease" }
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	host := ID()
	dial := func(role string) (*websocket.Conn, Frame) {
		t.Helper()
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/p2p/signal?id="+host+"&role="+role, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token"}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ws.CloseNow() })
		_, raw, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var f Frame
		if err = json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		return ws, f
	}
	old, _ := dial("host")
	replacement, welcome := dial("host")
	if _, _, err := old.Read(ctx); err == nil {
		t.Fatal("old host socket not retired")
	}
	if !s.Online(host) {
		t.Fatal("old socket cleanup removed replacement room")
	}
	client, cw := dial("client")
	if err := client.Write(ctx, websocket.MessageText, []byte(`{"kind":"renew"}`)); err != nil {
		t.Fatal(err)
	}
	_, raw, err := client.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lease Frame
	_ = json.Unmarshal(raw, &lease)
	if lease.Kind != "lease" || lease.Peer != cw.Peer || lease.Grant != "signed-test-lease" {
		t.Fatal(lease)
	}
	if _, _, err = replacement.Read(ctx); err != nil {
		t.Fatal("new host lost its controller lease", err)
	}
	// Relay attachment must bind the exact live signal peer, even for the host.
	data, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/p2p/relay?id="+host+"&role=host&peer="+welcome.Peer, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer data.CloseNow()
	if _, _, err = data.Read(ctx); err != nil {
		t.Fatal(err)
	}
	next, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/p2p/relay?id="+host+"&role=host&peer="+welcome.Peer, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer next.CloseNow()
	if _, _, err = next.Read(ctx); err != nil {
		t.Fatal("replacement relay failed", err)
	}
	if _, _, err = data.Read(ctx); err == nil {
		t.Fatal("old relay not retired")
	}
	if !s.Online(host) {
		t.Fatal("relay replacement disconnected host signal")
	}
}
