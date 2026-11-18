//go:build unix

package codex

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func fakeCLI(t *testing.T, metadata string) (*Provider, string) {
	t.Helper()
	dir := t.TempDir()
	state, calls := filepath.Join(dir, "state"), filepath.Join(dir, "calls")
	t.Setenv("AGENTHUB_TEST_CLI_STATE", state)
	t.Setenv("AGENTHUB_TEST_CLI_CALLS", calls)
	path := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AGENTHUB_TEST_CLI_CALLS\"\nif [ \"$*\" = \"app-server daemon version\" ]; then\ncat \"$AGENTHUB_TEST_CLI_STATE\"\nelif [ \"$*\" = \"app-server daemon start\" ]; then\ncp \"$AGENTHUB_TEST_CLI_STATE.next\" \"$AGENTHUB_TEST_CLI_STATE\"\nelse\nexit 1\nfi\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte(metadata), 0600); err != nil {
		t.Fatal(err)
	}
	// Allow macOS to finish first-execution validation of a generated executable
	// during fixture setup, outside the production metadata probe's 3s deadline.
	setup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := exec.CommandContext(setup, path, "app-server", "daemon", "version").Output(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(calls); err != nil {
		t.Fatal(err)
	}
	p := NewCLI(config.Config{CodexCLI: path}, events.New())
	t.Cleanup(func() { _ = p.Close() })
	return p, calls
}

func TestCLIDiscoveryDoesNotClaimReadyOrStartDaemon(t *testing.T) {
	p, calls := fakeCLI(t, "{\"status\":\"running\",\"socketPath\":\"/nonexistent.sock\",\"appServerVersion\":\"test\"}")
	for i := 0; i < 2; i++ {
		s := p.Status(context.Background())
		if !s.Installed || !s.Running || s.Ready() || s.Version != "test" {
			t.Fatalf("running is distinct from connected: %+v", s)
		}
	}
	b, _ := os.ReadFile(calls)
	if string(b) != "app-server daemon version\n" {
		t.Fatalf("discovery commands: %s", b)
	}
	if err := os.WriteFile(os.Getenv("AGENTHUB_TEST_CLI_STATE"), []byte("{\"status\":\"stopped\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	p.probeMu.Lock()
	p.probeAt = time.Time{}
	p.probeMu.Unlock()
	if err := p.AutoConnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(calls)
	if strings.Contains(string(b), "start") {
		t.Fatal("automatic discovery launched a stopped daemon")
	}
}

func TestCLIAttachmentOnlyInitializesAndExplicitEnableCanStart(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		name := "running"
		if stopped {
			name = "stopped"
		}
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "ah-cli-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "s")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			methods := make(chan string, 4)
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, e := websocket.Accept(w, r, nil)
				if e != nil {
					return
				}
				defer ws.CloseNow()
				for {
					_, b, e := ws.Read(r.Context())
					if e != nil {
						return
					}
					var m rpc.Message
					if json.Unmarshal(b, &m) != nil {
						return
					}
					methods <- m.Method
					if m.Method == "initialize" {
						response, _ := json.Marshal(map[string]any{"id": m.ID, "result": map[string]any{}})
						_ = ws.Write(r.Context(), websocket.MessageText, response)
					}
				}
			})}
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			metadata, _ := json.Marshal(map[string]string{"status": "running", "socketPath": socket, "appServerVersion": "test"})
			initial := string(metadata)
			if stopped {
				initial = "{\"status\":\"stopped\"}"
			}
			p, calls := fakeCLI(t, initial)
			if stopped {
				if err = os.WriteFile(os.Getenv("AGENTHUB_TEST_CLI_STATE")+".next", metadata, 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if stopped {
				err = p.Start(ctx)
			} else {
				err = p.AutoConnect(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := p.Status(ctx); !s.Ready() || !s.Running {
				t.Fatalf("not ready after handshake: %+v", s)
			}
			for _, want := range []string{"initialize", "initialized"} {
				select {
				case got := <-methods:
					if got != want {
						t.Fatalf("got %s, want %s", got, want)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if err = p.AutoConnect(ctx); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(calls)
			if strings.Contains(string(b), "daemon start") != stopped {
				t.Fatalf("unexpected lifecycle commands: %s", b)
			}
			select {
			case m := <-methods:
				t.Fatalf("attachment invoked extra RPC %s", m)
			default:
			}
		})
	}
}
