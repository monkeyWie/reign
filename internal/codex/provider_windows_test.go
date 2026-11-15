package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
)

func TestWindowsInstalledAppReportsUnsupportedWithoutLaunching(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "ChatGPT.exe")
	if err := os.WriteFile(app, nil, 0600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "no-runtime")
	p := New(config.Config{DataDir: data, CodexApp: app}, "", events.New())
	ctx := context.Background()
	s := p.Status(ctx)
	if !s.Installed || s.Running || s.Ready() || s.State != "unsupported" || s.InstallPath != app || s.LastError != "" {
		t.Fatalf("installation conflated with remote support: %+v", s)
	}
	if err := p.Start(ctx); err == nil || !strings.Contains(err.Error(), "暂不支持接入") {
		t.Fatalf("enable: %v", err)
	}
	if err := p.ConnectDesktop(ctx, nil, true); err == nil || !strings.Contains(err.Error(), "暂不支持接入") {
		t.Fatalf("activate: %v", err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatalf("unsupported setup created runtime state: %v", err)
	}
}
