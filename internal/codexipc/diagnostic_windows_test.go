package codexipc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsPipeSourceAncestry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		processes map[uint32]pipeProcess
		want      string
	}{
		{"ssh through vscode and node", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {3, "Code.exe"}, 3: {0, "sshd.exe"}}, "SSH"},
		{"ssh session", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {0, "SSHD-SESSION.EXE"}}, "SSH"},
		{"vscode", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {0, "Code.exe"}}, "VS Code"},
		{"insiders", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {0, "Code - Insiders.exe"}}, "VS Code"},
		{"tunnel", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {0, "code-tunnel.exe"}}, "VS Code"},
		{"insiders tunnel", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {0, "code-tunnel-insiders.exe"}}, "VS Code"},
		{"node alone", map[uint32]pipeProcess{1: {0, "node.exe"}}, ""},
		{"unrelated ssh", map[uint32]pipeProcess{1: {0, "node.exe"}, 2: {0, "sshd.exe"}}, ""},
		{"unrecognized name", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {0, "not-code.exe"}}, ""},
		{"desktop", map[uint32]pipeProcess{1: {2, "Codex.exe"}, 2: {0, "explorer.exe"}}, ""},
		{"missing parent", map[uint32]pipeProcess{1: {2, "node.exe"}}, ""},
		{"missing server", map[uint32]pipeProcess{}, ""},
		{"self cycle", map[uint32]pipeProcess{1: {1, "node.exe"}}, ""},
		{"ancestor cycle", map[uint32]pipeProcess{1: {2, "node.exe"}, 2: {3, "cmd.exe"}, 3: {2, "cmd.exe"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pipeProcessSource(context.Background(), 1, tc.processes); got != tc.want {
				t.Fatalf("source = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("depth boundary", func(t *testing.T) {
		processes := make(map[uint32]pipeProcess)
		for pid := uint32(1); pid <= maxPipeAncestors; pid++ {
			processes[pid] = pipeProcess{pid + 1, "node.exe"}
		}
		processes[maxPipeAncestors+1] = pipeProcess{0, "sshd.exe"}
		if got := pipeProcessSource(context.Background(), 1, processes); got != "" {
			t.Fatalf("followed beyond depth limit: %q", got)
		}
		processes[maxPipeAncestors] = pipeProcess{0, "sshd.exe"}
		if got := pipeProcessSource(context.Background(), 1, processes); got != "SSH" {
			t.Fatalf("missed last permitted ancestor: %q", got)
		}
	})
}

func TestWindowsPipeDiagnosticPreservesCause(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "fixture", Err: windows.ERROR_ACCESS_DENIED}
	for _, source := range []string{"SSH", "VS Code"} {
		t.Run(source, func(t *testing.T) {
			err := diagnosePipeAccess(context.Background(), "fixture", cause, func(context.Context, string) (uint32, string, error) {
				return 42, source, nil
			})
			var diagnostic *pipeAccessError
			var original *os.PathError
			if !errors.As(err, &diagnostic) || !errors.As(err, &original) || original != cause ||
				!errors.Is(err, windows.ERROR_ACCESS_DENIED) || !errors.Is(err, os.ErrPermission) {
				t.Fatalf("lost diagnostic or original cause: %v", err)
			}
			for _, hint := range []string{"可能来自 " + source, "PID 42", "在该环境中关闭使用 Codex 的实例", "重新打开桌面 Codex App"} {
				if !strings.Contains(diagnostic.ConnectionGuidance(), hint) {
					t.Fatalf("missing %q: %s", hint, diagnostic.ConnectionGuidance())
				}
			}
		})
	}
	for _, cause := range []error{nil, windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PIPE_BUSY, context.Canceled, context.DeadlineExceeded} {
		if got := diagnosePipeAccess(context.Background(), "fixture", cause, func(context.Context, string) (uint32, string, error) {
			t.Fatal("diagnosed a successful or non-permission result")
			return 0, "", nil
		}); got != cause {
			t.Fatalf("changed unrelated result: %v", got)
		}
	}
	for _, tc := range []struct {
		name    string
		pid     uint32
		source  string
		failure error
	}{
		{"unknown source", 42, "", nil},
		{"missing PID", 0, "SSH", nil},
		{"snapshot failed", 42, "SSH", errors.New("fixture snapshot failure")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := diagnosePipeAccess(context.Background(), "fixture", cause, func(context.Context, string) (uint32, string, error) {
				return tc.pid, tc.source, tc.failure
			})
			checkWindowsPipeFallback(t, got, cause, tc.pid)
			if tc.failure != nil && (!strings.Contains(got.Error(), tc.failure.Error()) || errors.Is(got, tc.failure)) {
				t.Fatalf("inspection failure must be visible without replacing the dial error: %v", got)
			}
		})
	}
	t.Run("metadata query failed", func(t *testing.T) {
		path := fmt.Sprintf(`\\.\pipe\reignhub-missing-fixture-%d-%d`, os.Getpid(), time.Now().UnixNano())
		got := diagnosePipeAccess(context.Background(), path, cause, inspectPipeSource)
		checkWindowsPipeFallback(t, got, cause, 0)
		if !strings.Contains(got.Error(), "打开连接服务的元数据句柄失败") || errors.Is(got, os.ErrNotExist) {
			t.Fatalf("lost query failure stage or replaced original permission error: %v", got)
		}
	})
}

func checkWindowsPipeFallback(t *testing.T, err error, cause *os.PathError, pid uint32) {
	t.Helper()
	var diagnostic interface{ ConnectionGuidance() string }
	var original *os.PathError
	if !errors.As(err, &diagnostic) || !errors.Is(err, windows.ERROR_ACCESS_DENIED) || !errors.Is(err, os.ErrPermission) || !errors.As(err, &original) || original != cause {
		t.Fatalf("lost permission cause or recovery guidance: %v", err)
	}
	hint := diagnostic.ConnectionGuidance()
	for _, text := range []string{"暂未确认", "SSH", "VS Code", "如有", "关闭该实例", "重新打开桌面 Codex App", "相同权限级别"} {
		if !strings.Contains(hint, text) {
			t.Fatalf("unknown source lost %q: %s", text, hint)
		}
	}
	if strings.Contains(hint, "检测到") || strings.Contains(hint, "可能来自") || strings.Contains(hint, "PID") != (pid != 0) || pid != 0 && !strings.Contains(hint, fmt.Sprintf("PID %d", pid)) {
		t.Fatalf("unknown source fabricated identification or lost a known PID: %s", hint)
	}
}

func TestWindowsPipeDiagnosticCancellation(t *testing.T) {
	cause := &os.PathError{Op: "open", Path: "fixture", Err: windows.ERROR_ACCESS_DENIED}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := diagnosePipeAccess(ctx, "fixture", cause, func(context.Context, string) (uint32, string, error) {
		t.Fatal("inspected after cancellation")
		return 0, "", nil
	})
	checkWindowsPipeFallback(t, got, cause, 0)
	if !strings.Contains(got.Error(), context.Canceled.Error()) || errors.Is(got, context.Canceled) {
		t.Fatalf("inspection cancellation replaced the dial failure: %v", got)
	}
	if _, err := pipeServerProcessID(ctx, "fixture"); !errors.Is(err, context.Canceled) {
		t.Fatalf("PID lookup ignored cancellation: %v", err)
	}
	if _, err := snapshotPipeProcesses(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot ignored cancellation: %v", err)
	}
	if source := pipeProcessSource(ctx, 1, map[uint32]pipeProcess{1: {0, "sshd.exe"}}); source != "" {
		t.Fatalf("ancestry ignored cancellation: %q", source)
	}
	t.Run("cancel during inspection", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		got := diagnosePipeAccess(ctx, "fixture", cause, func(context.Context, string) (uint32, string, error) {
			cancel()
			return 42, "SSH", nil
		})
		checkWindowsPipeFallback(t, got, cause, 0)
		if !strings.Contains(got.Error(), context.Canceled.Error()) {
			t.Fatalf("lost cancellation detail: %v", got)
		}
	})
	t.Run("budget", func(t *testing.T) {
		got := diagnosePipeAccess(context.Background(), "fixture", cause, func(ctx context.Context, _ string) (uint32, string, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > pipeDiagnosticBudget {
				t.Fatal("inspection has no bounded budget")
			}
			<-ctx.Done()
			return 42, "SSH", nil
		})
		checkWindowsPipeFallback(t, got, cause, 0)
		if !strings.Contains(got.Error(), context.DeadlineExceeded.Error()) || errors.Is(got, context.DeadlineExceeded) {
			t.Fatalf("inspection timeout replaced the dial failure: %v", got)
		}
	})
}
