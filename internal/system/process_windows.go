package system

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"github.com/monkeyWie/reignhub/internal/provider"
	"golang.org/x/sys/windows"
)

var errProcessExited = errors.New("process is no longer running")

// ProcessStamp uses one process handle for both fields, so a recycled PID
// cannot combine the executable of one generation with another's start time.
func ProcessStamp(ctx context.Context, pid int) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if pid <= 0 {
		return "", "", fmt.Errorf("invalid process ID")
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", "", err
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return "", "", err
	}
	if exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return "", "", errProcessExited
	}
	stamp := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	return strconv.FormatUint(stamp, 10), windows.UTF16ToString(buf[:size]), nil
}

// Discover is passive and matches full executable paths. Electron subprocesses
// share the main executable, so only roots of that executable's process tree
// count as desktop instances; CLI/app-server processes never count as the App.
func Discover(ctx context.Context, app string) ([]provider.Instance, error) {
	instances := []provider.Instance{}
	executable := AppExecutable(app)
	if executable == "" {
		return instances, nil
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return instances, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	parents := map[int]int{}
	candidates := []provider.Instance{}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if err := ctx.Err(); err != nil {
			return instances, err
		}
		pid := int(entry.ProcessID)
		parents[pid] = int(entry.ParentProcessID)
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), filepath.Base(executable)) {
			continue
		}
		started, path, e := ProcessStamp(ctx, pid)
		if e != nil {
			// A process may exit between the snapshot and opening its handle.
			if errors.Is(e, windows.ERROR_INVALID_PARAMETER) || errors.Is(e, errProcessExited) {
				continue
			}
			return instances, fmt.Errorf("读取 Codex App 进程 %d 失败：%w", pid, e)
		}
		if strings.EqualFold(filepath.Clean(path), filepath.Clean(executable)) {
			candidates = append(candidates, provider.Instance{PID: pid, StartedAt: started, Path: filepath.Clean(app)})
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return instances, err
	}
	return desktopRoots(candidates, parents), nil
}

func desktopRoots(candidates []provider.Instance, parents map[int]int) []provider.Instance {
	matched := map[int]bool{}
	for _, p := range candidates {
		matched[p.PID] = true
	}
	instances := []provider.Instance{}
	for _, p := range candidates {
		seen := map[int]bool{p.PID: true}
		child := false
		for parent := parents[p.PID]; parent != 0 && !seen[parent]; parent = parents[parent] {
			if matched[parent] {
				child = true
				break
			}
			seen[parent] = true
		}
		if !child {
			instances = append(instances, p)
		}
	}
	return instances
}
