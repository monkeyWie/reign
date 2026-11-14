package system

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/monkeyWie/reignhub/internal/provider"
)

// ProcessStamp identifies a process generation without reading its environment.
func ProcessStamp(ctx context.Context, pid int) (string, string, error) {
	if runtime.GOOS == "windows" {
		return "", "", fmt.Errorf("desktop runtime discovery currently supports macOS")
	}
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

// DesktopConnections checks the official App's sockets, not its process name
// alone. This does not read other clients' messages or proxy their connection.
func DesktopConnections(ctx context.Context, instances []provider.Instance, endpoint string) (int, error) {
	if runtime.GOOS != "darwin" {
		return 0, fmt.Errorf("desktop connection detection currently supports macOS")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Port() == "" {
		return 0, fmt.Errorf("invalid app-server endpoint")
	}
	peer := net.JoinHostPort(u.Hostname(), u.Port())
	connected := 0
	for _, instance := range instances {
		out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-a", "-p", strconv.Itoa(instance.PID), "-iTCP:"+u.Port(), "-sTCP:ESTABLISHED", "-Fn").Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
				continue
			}
			return 0, err
		}
		if connectedToPeer(string(out), peer) {
			connected++
		}
	}
	return connected, nil
}

func connectedToPeer(output, peer string) bool {
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		_, remote, ok := strings.Cut(line[1:], "->")
		if ok && remote == peer {
			return true
		}
	}
	return false
}
