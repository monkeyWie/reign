package codex

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/rpc"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTurnStatusDoesNotLoadMetadataOrImportMessageAttachments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			_, raw, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var m rpc.Message
			_ = json.Unmarshal(raw, &m)
			if m.Method == "initialized" {
				continue
			}
			result := json.RawMessage(`{}`)
			if m.Method == "thread/turns/list" {
				var args struct{ ItemsView string }
				if json.Unmarshal(m.Params, &args) != nil || args.ItemsView != "notLoaded" {
					t.Error("status requested item bodies")
				}
				result = json.RawMessage(`{"data":[{"id":"task","status":"completed","items":[{"type":"userMessage","content":[{"type":"localImage","path":"/unavailable/image.png"}]}]}]}`)
			} else if m.Method != "initialize" {
				t.Errorf("status check performed unrelated RPC %s", m.Method)
				return
			}
			b, _ := json.Marshal(map[string]any{"id": m.ID, "result": result})
			_ = ws.Write(r.Context(), websocket.MessageText, b)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := rpc.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{client: c}
	defer p.Close()
	state, err := p.TurnStatus(ctx, "session", "task")
	if err != nil || state.ID != "task" || state.Status != "completed" || len(state.Items) != 0 {
		t.Fatal("invalid turn status", state, err)
	}
}
