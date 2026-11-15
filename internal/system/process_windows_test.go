package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/provider"
)

func TestWindowsNativeProcessProbe(t *testing.T) {
	ctx := context.Background()
	started, path, err := ProcessStamp(ctx, os.Getpid())
	if err != nil || started == "" {
		t.Fatalf("current process stamp: %q %q %v", started, path, err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(path, exe) {
		t.Fatalf("image path %q want %q", path, exe)
	}
	instances, err := Discover(ctx, exe)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].PID != os.Getpid() || instances[0].StartedAt != started {
		t.Fatalf("native discovery of isolated test process: %+v", instances)
	}
	other := filepath.Join(t.TempDir(), filepath.Base(exe))
	if err := os.WriteFile(other, nil, 0600); err != nil {
		t.Fatal(err)
	}
	instances, err = Discover(ctx, other)
	if err != nil || len(instances) != 0 {
		t.Fatalf("same name at unrelated path matched: %+v %v", instances, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := ProcessStamp(canceled, os.Getpid()); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestWindowsDesktopProcessTreeRoots(t *testing.T) {
	candidates := []provider.Instance{{PID: 10}, {PID: 11}, {PID: 13}, {PID: 20}}
	parents := map[int]int{10: 1, 11: 10, 12: 10, 13: 12, 20: 2}
	got := desktopRoots(candidates, parents)
	if len(got) != 2 || got[0].PID != 10 || got[1].PID != 20 {
		t.Fatalf("Electron subprocesses counted as App instances: %+v", got)
	}
}
