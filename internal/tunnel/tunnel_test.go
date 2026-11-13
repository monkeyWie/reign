package tunnel

import (
	"context"
	"github.com/coder/websocket"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/remote"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEndpointAuthorization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := remote.Options{RelayOnly: true, ResolveKey: func(string) (string, bool) { return strings.Repeat("ab", 32), true }}
	h, err := Listen(ctx, "127.0.0.1:0", strings.Repeat("a", 32), opts, &control.Dispatcher{}, func(s string) (string, bool) { return "client", s == "derived-credential" })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	resp, err := http.Get(h.URL + "/v2/register")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("plaintext API exposed")
	}
	attempt, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	ws, _, err := websocket.Dial(attempt, strings.Replace(h.URL, "http", "ws", 1)+"/relay?id="+strings.Repeat("a", 32)+"&role=host", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer derived-credential"}}})
	if err == nil {
		ws.CloseNow()
		t.Fatal("client impersonated loopback host")
	}
	if bad, e := Listen(ctx, "0.0.0.0:0", strings.Repeat("a", 32), opts, &control.Dispatcher{}, nil); e == nil {
		bad.Close()
		t.Fatal("public origin listener admitted")
	}
}
func TestRunnerLifecycleAndPrivateToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test helper")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-cloudflared")
	script := "#!/bin/sh\nfor arg do case \"$arg\" in *secret-token*) exit 9;; esac; done\necho 'https://example.trycloudflare.com' >&2\necho 'Registered tunnel connection' >&2\nexec cat\n"
	// cat has immediate EOF under os/exec. The writer must drain the last log line
	// even when a connector exits immediately after printing it.
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var statuses []Status
	report := func(s Status) { mu.Lock(); defer mu.Unlock(); statuses = append(statuses, s) }
	if err := runOnce(context.Background(), Config{Binary: binary}, "http://127.0.0.1:42", report); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range statuses {
		if s.State == "connected" && s.URL == "https://example.trycloudflare.com" {
			found = true
		}
	}
	if !found {
		t.Fatalf("lost connector readiness logs: %+v", statuses)
	}
	path, err := TokenFile(dir, "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = TokenFile(dir, "replacement"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	st, _ := os.Stat(path)
	if string(b) != "replacement" || st.Mode().Perm() != 0600 {
		t.Fatal("token write insecure or incomplete")
	}
	if err := runOnce(context.Background(), Config{Binary: binary, TokenFile: path, PublicURL: "https://fixed.example"}, "http://127.0.0.1:42", report); err != nil {
		t.Fatal(err)
	}
}
