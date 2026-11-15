package connectivity

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketUpgradeWithHTTP2EnabledServer(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			fmt.Fprint(w, r.ProtoMajor)
			return
		}
		if r.ProtoMajor != 1 {
			t.Errorf("WebSocket upgrade used %s", r.Proto)
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer ws.CloseNow()
		if err := ws.Write(r.Context(), websocket.MessageText, []byte("connected")); err != nil {
			t.Error(err)
		}
		_, _, _ = ws.Read(r.Context())
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, upgrade := range []bool{false, true} {
		client := newConnectionHTTPClient(upgrade)
		transport := client.Transport.(*http.Transport)
		transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		defer transport.CloseIdleConnections()
		// Prime the pool before upgrading: a reusable HTTP/2 connection must never
		// capture the HTTP/1.1 WebSocket request.
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := "2"
		if upgrade {
			want = "1"
		}
		if string(body) != want {
			t.Fatalf("upgrade=%t HTTP version %s, want %s", upgrade, body, want)
		}
		if !upgrade {
			continue
		}
		ws, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/ws", &websocket.DialOptions{HTTPClient: client})
		if err != nil {
			t.Fatal("HTTP/2-capable service rejected WebSocket upgrade:", err)
		}
		_, message, err := ws.Read(ctx)
		ws.CloseNow()
		if err != nil || string(message) != "connected" {
			t.Fatalf("WebSocket data: %s, %v", message, err)
		}
	}
}
