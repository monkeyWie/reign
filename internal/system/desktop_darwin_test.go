//go:build darwin

package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Exercise the actual macOS AppleEvent path against a temporary application,
// including paths with spaces, and ensure refusal never triggers a force quit.
func TestQuitDesktopAppleEvent(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprintf("refuse=%v", refuse), func(t *testing.T) {
			dir := t.TempDir()
			app := filepath.Join(dir, "Agent Hub quit fixture.app")
			started := filepath.Join(dir, "started")
			stopped := filepath.Join(dir, "stopped")
			block := filepath.Join(dir, "refuse")
			source := fmt.Sprintf(`on markFile(p)
 set f to open for access (POSIX file p) with write permission
 write "ok" to f
 close access f
end markFile
on run
 my markFile(%s)
end run
on idle
 return 1
end idle
on quit
 set shouldRefuse to false
 try
  set blockFile to (POSIX file %s) as alias
  set shouldRefuse to true
 end try
 if shouldRefuse then error number -128
 my markFile(%s)
 continue quit
end quit`, strconv.Quote(started), strconv.Quote(block), strconv.Quote(stopped))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/usr/bin/osacompile", "-s", "-o", app, "-e", source)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("compile isolated app: %s (%v)", out, err)
			}
			if refuse {
				if err := os.WriteFile(block, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd = exec.CommandContext(ctx, "/usr/bin/open", "-n", app)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("launch isolated app: %s (%v)", out, err)
			}
			t.Cleanup(func() {
				if _, err := os.Stat(stopped); err == nil {
					return
				}
				_ = os.Remove(block)
				cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				_ = quitDesktop(cleanup, app)
			})
			waitDesktopFixtureFile(t, started)
			err := quitDesktop(ctx, app)
			if refuse {
				if err == nil {
					t.Fatal("expected normal quit refusal")
				}
				if _, err := os.Stat(stopped); !os.IsNotExist(err) {
					t.Fatal("application must stay running after refusing quit")
				}
				_ = os.Remove(block)
				if err := quitDesktop(ctx, app); err != nil {
					t.Fatalf("app no longer available after refusing: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			waitDesktopFixtureFile(t, stopped)
		})
	}
}

func waitDesktopFixtureFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
