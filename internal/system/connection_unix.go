//go:build !windows

package system

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ProcessStamp identifies a process generation without reading its environment.
func ProcessStamp(ctx context.Context, pid int) (string, string, error) {
	out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "lstart=,comm=").Output()
	if err != nil {
		return "", "", err
	}
	parts := strings.Fields(string(out))
	if len(parts) < 6 {
		return "", "", fmt.Errorf("process is no longer running")
	}
	return strings.Join(parts[:5], " "), strings.Join(parts[5:], " "), nil
}
