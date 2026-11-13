package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/events"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/rpc"
)

// The same test executable acts as a harmless official-server fixture. It never
// reads Codex data, launches a real Agent, resumes a conversation or runs a model.
func TestMain(m *testing.M) {
	if os.Getenv("AGENTHUB_FAKE_CODEX_RUNTIME") == "1" && len(os.Args) > 1 && os.Args[1] == "-c" {
		u, err := url.Parse(os.Args[len(os.Args)-1])
		if err != nil {
			os.Exit(2)
		}
		ln, err := net.Listen("tcp", u.Host)
		if err != nil {
			os.Exit(2)
		}
		err = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				var msg rpc.Message
				if json.Unmarshal(raw, &msg) != nil {
					return
				}
				if msg.Method == "initialized" {
					continue
				}
				result := json.RawMessage(`{}`)
				if msg.Method == "echo" {
					result = msg.Params
				}
				response, _ := json.Marshal(map[string]any{"id": msg.ID, "result": result})
				if ws.Write(r.Context(), websocket.MessageText, response) != nil {
					return
				}
			}
		}))
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRuntimeLauncherProcess(t *testing.T) {
	if os.Getenv("AGENTHUB_TEST_RUNTIME_LAUNCHER") != "1" {
		return
	}
	dir := os.Getenv("AGENTHUB_TEST_RUNTIME_DIR")
	p := New(config.Config{DataDir: dir, CodexCLI: os.Args[0]}, "", events.New())
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "exit-parent")); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestDirectSharedRuntimeSurvivesHubExitAndIsReused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS/Unix process fixture")
	}
	dir := t.TempDir()
	launcher := exec.Command(os.Args[0], "-test.run=^TestRuntimeLauncherProcess$")
	launcher.Env = append(os.Environ(), "AGENTHUB_FAKE_CODEX_RUNTIME=1", "AGENTHUB_TEST_RUNTIME_LAUNCHER=1", "AGENTHUB_TEST_RUNTIME_DIR="+dir)
	var output bytes.Buffer
	launcher.Stdout, launcher.Stderr = &output, &output
	if err := launcher.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = launcher.Process.Kill() })
	deadline := time.Now().Add(8 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture parent did not initialize")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var state sharedRuntime
	if err := device.Load(filepath.Join(dir, "codex-app-server.json"), &state); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if p, err := os.FindProcess(state.PID); err == nil {
			_ = p.Kill()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	app, err := rpc.Dial(ctx, state.Endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	echo := func(text string) {
		t.Helper()
		var result map[string]string
		if err := app.Call(ctx, "echo", map[string]string{"text": text}, &result); err != nil || result["text"] != text {
			t.Fatalf("direct App connection interrupted: %v %v", result, err)
		}
	}
	echo("before Hub exit")
	if err := os.WriteFile(filepath.Join(dir, "exit-parent"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := launcher.Wait(); err != nil {
		t.Fatalf("launcher: %v %s", err, &output)
	}
	echo("after Hub process exited")
	p := New(config.Config{DataDir: dir, CodexCLI: os.Args[0]}, "", events.New())
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if p.shared.PID != state.PID || p.endpoint != state.Endpoint {
		t.Fatal("Hub restart created a different backend")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	echo("after restarted Hub disconnected")
	if !runtimeAlive(ctx, state) {
		t.Fatal("shared server no longer alive")
	}
}

func TestOnlyHubLaunchedConnectedDesktopIsAdmitted(t *testing.T) {
	launched := []provider.Instance{{PID: 42, StartedAt: "today", Path: "/Applications/Codex.app"}}
	manual := []provider.Instance{{PID: 43, StartedAt: "today", Path: launched[0].Path}}
	recycled := []provider.Instance{{PID: 42, StartedAt: "tomorrow", Path: launched[0].Path}}
	for _, tc := range []struct {
		name              string
		current, admitted []provider.Instance
		connections       int
		want              bool
	}{
		{"Hub launch connected", launched, launched, 1, true},
		{"Hub launch disconnected", launched, launched, 0, false},
		{"manual launch despite same server", manual, launched, 1, false},
		{"no Hub launch record", manual, nil, 1, false},
		{"recycled PID", recycled, launched, 1, false},
		{"not running", nil, launched, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := admittedDesktop(tc.current, tc.admitted, tc.connections); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestStoppedRuntimeRestoresAddressAndDropsUnmatchedDesktop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix fake runtime fixture")
	}
	t.Setenv("AGENTHUB_FAKE_CODEX_RUNTIME", "1")
	cfg := config.Config{DataDir: t.TempDir(), CodexCLI: os.Args[0]}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first := New(cfg, "", events.New())
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	before := first.shared
	defer first.Close()
	proc, err := os.FindProcess(before.PID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proc.Kill() })
	if err := proc.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtimeAlive(ctx, before) {
		if time.Now().After(deadline) {
			t.Fatal("fake runtime did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Backend address restoration is portable; admitting or discarding a
	// macOS App instance is only supported by the macOS discovery adapter.
	if runtime.GOOS == "darwin" {
		before.Desktop = []provider.Instance{{PID: 42, StartedAt: "obsolete", Path: "/nonexistent/Agent.app"}}
	}
	if err := device.Save(first.runtimeFile(), before); err != nil {
		t.Fatal(err)
	}
	second := New(cfg, "", events.New())
	if err := second.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	t.Cleanup(func() {
		if p, err := os.FindProcess(second.shared.PID); err == nil {
			_ = p.Kill()
		}
	})
	if second.shared.PID == before.PID || second.endpoint != before.Endpoint {
		t.Fatal("replacement must use a new process and the existing App's address")
	}
	if len(second.shared.Desktop) != 0 {
		t.Fatal("replacement retained an unmatched desktop process")
	}
	var result map[string]string
	if err := second.call(ctx, "echo", map[string]string{"text": "reconnected"}, &result); err != nil || result["text"] != "reconnected" {
		t.Fatalf("replacement runtime is not usable: %v", err)
	}
}

func TestStoppedRuntimeDoesNotMoveBehindAppWhenAddressOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	p := New(config.Config{DataDir: t.TempDir(), CodexCLI: os.Args[0]}, "", events.New())
	if err := device.Save(p.runtimeFile(), sharedRuntime{
		Endpoint: "ws://" + listener.Addr().String(), PID: -1, CLI: os.Args[0],
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "未启动替代后端") {
		t.Fatalf("occupied address should fail without creating a different backend: %v", err)
	}
	if p.client != nil {
		t.Fatal("unexpected backend connection")
	}
}

func TestRuntimeMetadataRejectsNonlocalOrWrongProcess(t *testing.T) {
	for _, endpoint := range []string{"wss://example.com:4500", "ws://0.0.0.0:4500", "ws://127.0.0.1:4500/desktop/key", "ws://user@127.0.0.1:4500", "ws://127.0.0.1:0", "ws://127.0.0.1:99999"} {
		if validRuntimeEndpoint(endpoint) {
			t.Error("accepted", endpoint)
		}
	}
	if !validRuntimeEndpoint("ws://127.0.0.1:4500") {
		t.Fatal("rejected official local endpoint")
	}
	if runtimeAlive(context.Background(), sharedRuntime{Endpoint: "ws://127.0.0.1:4500", PID: os.Getpid(), StartedAt: "different generation", Executable: strings.TrimSpace(os.Args[0])}) {
		t.Fatal("accepted a different process generation")
	}
}
