package codexipc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	pipeDiagnosticBudget = 200 * time.Millisecond
	maxPipeProcesses     = 16384
	maxPipeAncestors     = 32
)

// The original dial error remains the cause; diagnostic failures never replace it.
type pipeAccessError struct {
	cause     error
	serverPID uint32
	source    string
}

func (e *pipeAccessError) Error() string { return e.cause.Error() }
func (e *pipeAccessError) Unwrap() error { return e.cause }
func (e *pipeAccessError) ConnectionGuidance() string {
	return fmt.Sprintf("Codex 连接访问被拒绝，检测到连接服务可能来自 %s 环境（PID %d）。请在该环境中关闭使用 Codex 的实例，然后重新打开桌面 Codex App 并重试", e.source, e.serverPID)
}

func diagnosePipeAccess(ctx context.Context, path string, cause error, inspect func(context.Context, string) (uint32, string)) error {
	if !errors.Is(cause, windows.ERROR_ACCESS_DENIED) || ctx.Err() != nil {
		return cause
	}
	ctx, cancel := context.WithTimeout(ctx, pipeDiagnosticBudget)
	defer cancel()
	pid, source := inspect(ctx, path)
	if ctx.Err() != nil || pid == 0 || source == "" {
		return cause
	}
	return &pipeAccessError{cause: cause, serverPID: pid, source: source}
}

func inspectPipeSource(ctx context.Context, path string) (uint32, string) {
	pid, err := pipeServerProcessID(ctx, path)
	if err != nil || pid == 0 {
		return 0, ""
	}
	processes, err := snapshotPipeProcesses(ctx)
	if err != nil {
		return 0, ""
	}
	return pid, pipeProcessSource(ctx, pid, processes)
}

func pipeServerProcessID(ctx context.Context, path string) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	// Zero desired access opens metadata only, without data read/write or server
	// rights. Do not wait for a busy pipe or allow caller impersonation. This handle
	// is closed before taking the process snapshot; communication stays in winio.
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_ANONYMOUS, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(h)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(h, &pid); err != nil {
		return 0, err
	}
	return pid, ctx.Err()
}

type pipeProcess struct {
	parent uint32
	name   string
}

func snapshotPipeProcesses(ctx context.Context) (map[uint32]pipeProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	processes := make(map[uint32]pipeProcess)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(h, &entry); err == nil; err = windows.Process32Next(h, &entry) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(processes) >= maxPipeProcesses {
			return nil, errors.New("pipe diagnostic process limit reached")
		}
		processes[entry.ProcessID] = pipeProcess{parent: entry.ParentProcessID, name: windows.UTF16ToString(entry.ExeFile[:])}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, err
	}
	return processes, ctx.Err()
}

func pipeProcessSource(ctx context.Context, pid uint32, processes map[uint32]pipeProcess) string {
	seen := make(map[uint32]bool)
	source := ""
	for depth := 0; pid != 0 && depth < maxPipeAncestors; depth++ {
		if ctx.Err() != nil {
			return ""
		}
		if seen[pid] {
			break
		}
		seen[pid] = true
		p, ok := processes[pid]
		if !ok {
			break
		}
		switch strings.ToLower(p.name) {
		case "sshd.exe", "sshd-session.exe":
			return "SSH"
		case "code.exe", "code - insiders.exe", "code-tunnel.exe", "code-tunnel-insiders.exe":
			// Keep looking: VS Code can itself have an SSH ancestor. Names and
			// parent PIDs are clues, not proof of origin (parents can exit/reuse PIDs).
			source = "VS Code"
		}
		pid = p.parent
	}
	return source
}
