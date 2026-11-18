package codex

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
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestRemoveArchivesExactThreadAndRejectsRunningWork(t *testing.T) {
	var busy, loseReply atomic.Bool
	var archives atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ws, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		for {
			_, raw, err := ws.Read(req.Context())
			if err != nil {
				return
			}
			var message rpc.Message
			_ = json.Unmarshal(raw, &message)
			if message.Method == "initialized" {
				continue
			}
			result := any(map[string]any{})
			switch message.Method {
			case "initialize":
			case "thread/read", "thread/archive":
				var params struct {
					ThreadID string `json:"threadId"`
				}
				_ = json.Unmarshal(message.Params, &params)
				if params.ThreadID != "chosen" {
					t.Error("wrong thread", params.ThreadID)
					return
				}
				if message.Method == "thread/read" {
					status := "idle"
					if busy.Load() {
						status = "active"
					}
					result = map[string]any{"thread": map[string]any{"id": "chosen", "status": map[string]any{"type": status}}}
				} else {
					archives.Add(1)
					if loseReply.Load() {
						return
					}
				}
			default:
				t.Error("unexpected ownership/execution RPC", message.Method)
				return
			}
			response, _ := json.Marshal(map[string]any{"id": message.ID, "result": result})
			_ = ws.Write(req.Context(), websocket.MessageText, response)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := rpc.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &Provider{client: c}
	defer p.Close()
	busy.Store(true)
	if err := p.RemoveSession(ctx, "chosen"); err == nil || archives.Load() != 0 {
		t.Fatal("running thread was archived", err)
	}
	busy.Store(false)
	if err := p.RemoveSession(ctx, "chosen"); err != nil || archives.Load() != 1 {
		t.Fatal(err, archives.Load())
	}
	loseReply.Store(true)
	if err := p.RemoveSession(ctx, "chosen"); !control.OutcomeUnknown(err) || archives.Load() != 2 {
		t.Fatal("lost acknowledgement allowed replay", err)
	}
}
