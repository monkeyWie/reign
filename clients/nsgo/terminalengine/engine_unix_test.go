//go:build (darwin && !ios) || (linux && !android)

package terminalengine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/monkeyWie/reignhub/internal/terminal"
	"github.com/nsgo-dev/nsgo/base/terminal"
)

func TestTerminalOwnedShellAndMirror(t *testing.T) {
	dir := t.TempDir()
	// A non-login fixture shell avoids the user's shell profiles and Agents.
	e, err := newEngine(context.Background(), terminal.Options{Dir: dir, Command: []string{"/bin/sh", "-c", `printf 'TERMINAL_READY\n'; read -r value; printf '\nRESULT:%s\n' "$value"; printf 'CWD:%s\n' "$PWD"`}, Scrollback: 64 << 10}, model.Size{Cols: 180, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	receiver, err := terminal.New(terminal.Options{Conn: &fixtureConn{done: make(chan struct{})}, ResizeByConn: true})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	receiver.Resize(180, 24)
	deadline := time.Now().Add(4 * time.Second)
	for !strings.Contains(receiver.Text(), "TERMINAL_READY") && time.Now().Before(deadline) {
		receiver.Feed(e.Frame(false))
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(receiver.Text(), "TERMINAL_READY") {
		t.Fatal("shell did not render")
	}
	e.Send([]byte("fixture-value\r"))
	select {
	case <-e.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("shell did not exit")
	}
	receiver.Feed(e.Frame(false))
	if text := receiver.Text(); !strings.Contains(text, "RESULT:fixture-value") || !strings.Contains(text, dir) {
		t.Fatal("input/cwd not reflected", text)
	}
	if e.ExitCode() != 0 {
		t.Fatal("fixture failed", e.ExitCode())
	}
	// A fresh receiver reconstructs the same completed screen from a reset.
	receiver.Feed(e.Frame(true))
	if !strings.Contains(receiver.Text(), "RESULT:fixture-value") {
		t.Fatal("reset lost screen")
	}
}

type fixtureConn struct{ done chan struct{} }

func (c *fixtureConn) Read([]byte) (int, error)    { <-c.done; return 0, context.Canceled }
func (c *fixtureConn) Write(b []byte) (int, error) { return len(b), nil }
func (c *fixtureConn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

func TestTerminalSelectedUnixShellRunsInHostDirectory(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture-shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'PROFILE_SELECTED:%s\\n' \"$PWD\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", path)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	profiles, err := model.Profiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, profile := range profiles {
		if profile.Name == "fixture-shell" {
			id = profile.ID
		}
	}
	if id == "" {
		t.Fatal("fixture shell missing from host discovery")
	}
	m := model.New(ctx, func(ctx context.Context, directory string, size model.Size, shell string) (model.Engine, error) {
		if shell != path {
			return nil, fmt.Errorf("wrong profile executable %q", shell)
		}
		return New(ctx, directory, size, shell)
	})
	defer m.Close()
	owner := model.Scope{Owner: "fixture", Provider: "test", Session: "one"}
	p := model.Params{ID: strings.Repeat("ab", 32), Profile: id, Size: model.Size{Cols: 200, Rows: 24}}
	frame, err := m.Open(ctx, owner, p, func(context.Context) (string, error) { return dir, nil })
	if err != nil {
		t.Fatal(err)
	}
	var data strings.Builder
	for {
		data.Write(frame.Data)
		if strings.Contains(data.String(), "PROFILE_SELECTED:"+dir) {
			break
		}
		if frame.Exited {
			t.Fatal("selected shell did not render its directory", data.String())
		}
		p.Attachment, p.After = frame.Attachment, frame.Sequence
		frame, err = m.Read(ctx, owner, p)
		if err != nil {
			t.Fatal(err)
		}
	}
}
