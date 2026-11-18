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
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
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
				td := thread{ID: "original", Cwd: "/work/project", Source: json.RawMessage(tc.source), Originator: tc.originator, UpdatedAt: 42, CreatedAt: 12}
				s := p.session(td)
				if s.SourceKind != tc.want || s.Source != tc.source || s.Originator != tc.originator || s.ProviderID != wantProvider || s.ID != "original" || s.CWD != "/work/project" || s.UpdatedAt != 42000 || s.CreatedAt != 12000 {
					t.Fatalf("creation source or access route lost: %+v", s)
				}
			}
		})
	}
}

func TestSessionListExcludesSDKAndPreservesPagination(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
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
			case "project/list":
				result = json.RawMessage(`{"data":[{"id":"project","name":"Project","roots":[{"path":"/project"}]}]}`)
			case "thread/turns/list":
				result = json.RawMessage(`{"data":[]}`)
			case "thread/list":
				var params struct {
					SourceKinds                    []string
					SortKey, SortDirection, Cursor string
				}
				if err := json.Unmarshal(m.Params, &params); err != nil {
					t.Error(err)
					return
				}
				if strings.Join(params.SourceKinds, ",") != "cli,vscode,appServer,subAgent,unknown" || params.SortKey != "updated_at" || params.SortDirection != "desc" {
					t.Errorf("unexpected listing filters: %s", m.Params)
				}
				if params.Cursor == "" {
					result = json.RawMessage(`{"data":[{"id":"sdk","source":"appServer","originator":"codex_sdk_ts"},{"id":"app","cwd":"/project","source":"vscode","originator":"Codex Desktop","updatedAt":10},{"id":"exec","source":"exec"},{"id":"cli","cwd":"/project","source":"cli","updatedAt":9}],"nextCursor":"page-2"}`)
				} else if params.Cursor == "page-2" {
					result = json.RawMessage(`{"data":[{"id":"sdk-only-page","source":"exec","originator":"codex_sdk_ts"}],"nextCursor":"page-3"}`)
				} else {
					if params.Cursor != "page-3" {
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
	if first.Sessions[0].Project == nil || first.Sessions[0].Project.Name != "Project" {
		t.Fatal("project metadata missing from session page")
	}
	second, err := p.Sessions(ctx, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Sessions) != 0 || second.NextCursor != "page-3" {
		t.Fatalf("bad second page: %+v", second)
	}
	third, err := p.Sessions(ctx, second.NextCursor)
	if err != nil || len(third.Sessions) != 1 || third.Sessions[0].SourceKind != "subAgent" || third.NextCursor != "" {
		t.Fatalf("bad third page: %+v, %v", third, err)
	}
}

func TestIPCCreationAndActivityDatesRemainIndependent(t *testing.T) {
	td := stateThread(map[string]any{"id": "fixture", "createdAt": "2026-11-18T00:00:00Z", "updatedAt": float64(1794963600000)})
	if td.CreatedAt != 1794960000000 || td.UpdatedAt != 1794963600000 {
		t.Fatalf("IPC timestamps: %+v", td)
	}
	unknown := stateThread(map[string]any{"id": "fixture", "updatedAt": float64(1794963600000)})
	if unknown.CreatedAt != 0 {
		t.Fatal("missing creation time inferred from activity")
	}
}
