//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package codexipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

func Endpoint(home string) string { return filepath.Join(home, "ipc", "ipc.sock") }
func dialLocal(ctx context.Context, path string) (net.Conn, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if st.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("IPC endpoint is not a socket")
	}
	if st.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("IPC socket permits writes from other users")
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return nil, fmt.Errorf("IPC socket belongs to another user")
	}
	return (&net.Dialer{}).DialContext(ctx, "unix", path)
}
