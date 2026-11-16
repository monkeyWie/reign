package codex

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestWatchRejoinsSameThreadWithoutStartingTurn(t *testing.T) {
	connected := make(chan *websocket.Conn, 1)
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
			if m.Method != "initialize" && m.Method != "thread/resume" {
				t.Errorf("unexpected RPC method %s", m.Method)
				return
			}
			if m.Method == "thread/resume" && string(m.Params) != `{"excludeTurns":true,"threadId":"session"}` {
				t.Error("watch changed thread identity or runtime settings")
			}
			response, _ := json.Marshal(map[string]any{"id": m.ID, "result": map[string]any{}})
			_ = ws.Write(r.Context(), websocket.MessageText, response)
			if m.Method == "thread/resume" {
				connected <- ws
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
	client, err := rpc.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), p.observe)
	if err != nil {
		t.Fatal(err)
	}
	p.client = client
	defer p.Close()
	base, err := p.Watch(ctx, "session", "", 0)
	if err != nil || !base.Reset {
		t.Fatal("watch bootstrap", err)
	}
	ws := <-connected
	for _, message := range []string{
		`{"method":"item/started","params":{"threadId":"session","turnId":"turn","item":{"id":"message","type":"agentMessage","text":""}}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"session","turnId":"turn","itemId":"message","delta":"partial"}}`,
	} {
		if err := ws.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
			t.Fatal(err)
		}
	}
	for {
		batch, err := p.Watch(ctx, "session", base.Epoch, base.Cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range batch.Events {
			if event.Kind == "text" && event.Delta == "partial" {
				return
			}
		}
		base = batch
	}
}

func TestLivePresentationEventsAndUnicodeOffsets(t *testing.T) {
	p := &Provider{}
	base, _ := p.stream().log.Watch(context.Background(), "session", "", 0)
	notify := func(method, params string) {
		t.Helper()
		p.observeStream(rpc.Message{Method: method, Params: json.RawMessage(params)})
	}
	notify("turn/started", `{"threadId":"session","turn":{"id":"turn","status":"inProgress"}}`)
	notify("item/started", `{"threadId":"session","turnId":"turn","item":{"id":"message","type":"agentMessage","text":"","phase":"final_answer"}}`)
	notify("item/agentMessage/delta", `{"threadId":"session","turnId":"turn","itemId":"message","delta":"你😀"}`)
	notify("item/agentMessage/delta", `{"threadId":"session","turnId":"turn","itemId":"message","delta":"好"}`)
	notify("item/started", `{"threadId":"session","turnId":"turn","item":{"id":"reasoning","type":"reasoning","summary":[],"content":["RAW_REASONING_SENTINEL"]}}`)
	notify("item/reasoning/textDelta", `{"threadId":"session","turnId":"turn","itemId":"reasoning","delta":"RAW_REASONING_SENTINEL"}`)
	notify("item/reasoning/summaryTextDelta", `{"threadId":"session","turnId":"turn","itemId":"reasoning","delta":"显示摘要","summaryIndex":0}`)
	notify("item/started", `{"threadId":"session","turnId":"turn","item":{"id":"tool","type":"commandExecution","command":"ls","aggregatedOutput":"TOOL_OUTPUT_SENTINEL","status":"inProgress"}}`)
	notify("item/completed", `{"threadId":"session","turnId":"turn","item":{"id":"message","type":"agentMessage","text":"你😀好","phase":"final_answer"}}`)
	notify("turn/completed", `{"threadId":"session","turn":{"id":"turn","status":"completed"}}`)
	batch, err := p.Watch(context.Background(), "session", base.Epoch, base.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	deltas, summaries := 0, 0
	for _, event := range batch.Events {
		if event.Kind == "text" {
			want := 0
			if deltas == 1 {
				want = 3 // one BMP character + a surrogate pair
			}
			if event.Offset != want {
				t.Fatalf("UTF-16 offset = %d, want %d", event.Offset, want)
			}
			deltas++
		}
		if event.Kind == "summary" && event.Summary == "显示摘要" {
			summaries++
		}
	}
	encoded, _ := json.Marshal(batch)
	if strings.Contains(string(encoded), "SENTINEL") {
		t.Fatal("raw reasoning or tool output entered the presentation stream")
	}
	if deltas != 2 || summaries != 1 || len(p.stream().items) != 0 {
		t.Fatal("incomplete live lifecycle", deltas, summaries, string(encoded))
	}
}

func TestPlainUserItemDoesNotRedownloadHistory(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		snapshot      bool
	}{
		{"plain", `[{"type":"text","text":"hello🙂"}]`, false},
		{"image", `[{"type":"localImage","path":"/tmp/picture.png"}]`, true},
		{"file", `[{"type":"text","text":"# Files mentioned by the user:\n[file.apk](/tmp/file.apk)"}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Provider{}
			base, _ := p.stream().log.Watch(context.Background(), "session", "", 0)
			p.observeStream(rpc.Message{Method: "item/started", Params: json.RawMessage(`{"threadId":"session","turnId":"turn","item":{"id":"message","type":"userMessage","content":` + tc.content + `}}`)})
			batch, err := p.Watch(context.Background(), "session", base.Epoch, base.Cursor)
			if err != nil {
				t.Fatal(err)
			}
			items, snapshots := 0, 0
			for _, e := range batch.Events {
				if e.Kind == "item" {
					items++
				}
				if e.Kind == "snapshot" {
					snapshots++
				}
			}
			want := 0
			if tc.snapshot {
				want = 1
			}
			if items != 1 || snapshots != want {
				t.Fatalf("items=%d snapshots=%d, want 1/%d", items, snapshots, want)
			}
		})
	}
}

func TestSessionPreviewIsBoundedWithoutChangingSourceText(t *testing.T) {
	text := strings.Repeat("长😀", 1000)
	source := thread{ID: "session", Preview: text}
	result := source.session()
	if len([]rune(result.Preview)) != 241 || !strings.HasSuffix(result.Preview, "…") || source.Preview != text {
		t.Fatal("preview truncated incorrectly")
	}
}
