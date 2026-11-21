package terminalengine

import (
	"context"
	"strings"
	"testing"
	"time"

	model "github.com/monkeyWie/reignhub/internal/terminal"
)

func TestTerminalInstalledCmdStartsInConversationDirectory(t *testing.T) {
	profiles, err := model.Profiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range profiles {
		if p.ID == "cmd" {
			found = true
		}
	}
	if !found {
		t.Fatal("system cmd missing from installed profiles")
	}
	dir := t.TempDir()
	m := model.New(context.Background(), New)
	defer m.Close()
	scope := model.Scope{Owner: "fixture", Provider: "test", Session: "one"}
	p := model.Params{ID: strings.Repeat("ab", 32), Profile: "cmd", Size: model.Size{Cols: 160, Rows: 24}}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	frame, err := m.Open(ctx, scope, p, func(context.Context) (string, error) { return dir, nil })
	if err != nil {
		t.Fatal(err)
	}
	p.Attachment = frame.Attachment
	p.InputSeq, p.Data = 1, []byte("echo terminal-%CD%\r")
	if err = m.Input(ctx, scope, p); err != nil {
		t.Fatal(err)
	}
	var screen strings.Builder
	for !strings.Contains(strings.ToLower(screen.String()), strings.ToLower("terminal-"+dir)) {
		p.After = frame.Sequence
		frame, err = m.Read(ctx, scope, p)
		if err != nil {
			t.Fatal(err)
		}
		screen.Write(frame.Data)
	}
}
