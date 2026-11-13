package rpc

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDuplexAndDisconnect(t *testing.T) {
	approval := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		write := func(v any) { raw, _ := json.Marshal(v); _ = ws.Write(r.Context(), websocket.MessageText, raw) }
		for {
			_, raw, err := ws.Read(r.Context())
			if err != nil {
				return
			}
			var m Message
			_ = json.Unmarshal(raw, &m)
			if m.Method == "initialize" {
				write(map[string]any{"id": m.ID, "result": map[string]any{}})
			} else if m.Method == "initialized" {
				write(map[string]any{"id": "approval-1", "method": "item/commandExecution/requestApproval", "params": map[string]string{"command": "go test"}})
			} else if m.Method == "echo" {
				go func(id, params json.RawMessage) {
					time.Sleep(time.Millisecond)
					write(map[string]any{"id": id, "result": params})
				}(m.ID, m.Params)
			} else if m.Method == "disconnect" {
				return
			} else if string(m.ID) == `"approval-1"` {
				approval <- strings.Contains(string(m.Result), "accept")
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	c, err := Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), func(m Message, reply func(context.Context, any) error) {
		if err := reply(ctx, map[string]string{"decision": "accept"}); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var out struct{ N int }
			if err := c.Call(ctx, "echo", map[string]int{"n": n}, &out); err != nil {
				t.Error(err)
			} else if out.N != n {
				t.Error("response delivered to wrong caller")
			}
		}(i)
	}
	wg.Wait()
	select {
	case ok := <-approval:
		if !ok {
			t.Fatal("bad approval response")
		}
	case <-ctx.Done():
		t.Fatal("server request deadlocked")
	}
	if err = c.Call(ctx, "disconnect", nil, nil); err == nil {
		t.Fatal("disconnect did not fail pending call")
	}
}
