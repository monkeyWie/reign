package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/events"
	"github.com/monkeyWie/reign/internal/rpc"
)

func TestSessionCreationSourceIsIndependentOfAccessProvider(t *testing.T) {
	cases := []struct{ source, originator, want string }{
		{`"cli"`, "", "cli"},
		{`"vscode"`, "codex-tui", "cli"},
		{`"vscode"`, "Codex Desktop", "app"},
		{`"vscode"`, "codex_vscode", "vscode"},
		{`"vscode"`, "", "ideUnknown"},
		{`"vscode"`, "third-party-client", "custom"},
		{`"appServer"`, "", "appServer"},
		{`"appServer"`, "agenthub", "agenthub"},
		{`"exec"`, "", "exec"},
		{`"exec"`, "codex_cli_rs", "exec"},
		{`{"subAgent":{"thread_spawn":{"parent_thread_id":"parent","depth":1}}}`, "Codex Desktop", "subAgent"},
		{`{"subAgent":"review"}`, "codex_vscode", "subAgent"},
		{`{"subagent":"compact"}`, "", "subAgent"},
		{`{"custom":"client"}`, "", "custom"},
		{`"futureSource"`, "", "unknown"},
		{`null`, "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.source+"/"+tc.originator, func(t *testing.T) {
			for _, cli := range []bool{false, true} {
				p := New(config.Config{}, "", events.New())
				wantProvider := "codex-app"
				if cli {
					p.kind, wantProvider = "cli", "codex-cli"
				}
				td := thread{ID: "original", Cwd: "/work/project", Source: json.RawMessage(tc.source), Originator: tc.originator, UpdatedAt: 42}
				s := p.session(td)
				if s.SourceKind != tc.want || s.Source != tc.source || s.Originator != tc.originator || s.ProviderID != wantProvider || s.ID != "original" || s.CWD != "/work/project" || s.UpdatedAt != 42 {
					t.Fatalf("creation source or access route lost: %+v", s)
				}
			}
		})
	}
}

func TestSessionListIncludesAllSourcesAndPreservesPagination(t *testing.T) {
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
			if json.Unmarshal(raw, &m) != nil {
				return
			}
			var result any = map[string]any{}
			switch m.Method {
			case "initialize":
			case "initialized":
				continue
			case "thread/list":
				var params struct {
					SourceKinds                    []string
					SortKey, SortDirection, Cursor string
				}
				if err := json.Unmarshal(m.Params, &params); err != nil {
					t.Error(err)
					return
				}
				if strings.Join(params.SourceKinds, ",") != "cli,vscode,appServer,exec,subAgent,unknown" || params.SortKey != "updated_at" || params.SortDirection != "desc" {
					t.Errorf("unexpected listing filters: %s", m.Params)
				}
				if params.Cursor == "" {
					result = json.RawMessage(`{"data":[{"id":"app","cwd":"/project","source":"vscode","originator":"Codex Desktop","updatedAt":10},{"id":"cli","cwd":"/project","source":"cli","updatedAt":9}],"nextCursor":"page-2"}`)
				} else {
					if params.Cursor != "page-2" {
						t.Errorf("cursor lost: %s", params.Cursor)
					}
					result = json.RawMessage(`{"data":[{"id":"child","cwd":"/project","source":{"subAgent":"review"},"updatedAt":8}],"nextCursor":null}`)
				}
			default:
				t.Errorf("listing invoked extra method %s", m.Method)
			}
			response, _ := json.Marshal(map[string]any{"id": m.ID, "result": result})
			_ = ws.Write(r.Context(), websocket.MessageText, response)
		}
	}))
	defer server.Close()
	p := NewCLI(config.Config{}, events.New())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := rpc.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), p.observe)
	if err != nil {
		t.Fatal(err)
	}
	p.client = c
	defer p.Close()
	first, err := p.Sessions(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Sessions) != 2 || first.NextCursor != "page-2" || first.Sessions[0].SourceKind != "app" || first.Sessions[1].SourceKind != "cli" {
		t.Fatalf("bad first page: %+v", first)
	}
	second, err := p.Sessions(ctx, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Sessions) != 1 || second.Sessions[0].SourceKind != "subAgent" || second.NextCursor != "" {
		t.Fatalf("bad second page: %+v", second)
	}
}
