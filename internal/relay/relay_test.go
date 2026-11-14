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
			dial := func(path, role string) *websocket.Conn {
				t.Helper()
				ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+path+"?id="+host+"&role="+role, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer token"}}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { ws.CloseNow() })
				if _, _, err = ws.Read(ctx); err != nil {
					t.Fatal(err)
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
