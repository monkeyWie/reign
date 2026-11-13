//go:build unix

package connectivity

import (
	"golang.org/x/sys/unix"
	"syscall"
)

func reuseTCPPort(_, _ string, raw syscall.RawConn) error {
	var sockerr error
	err := raw.Control(func(fd uintptr) {
		sockerr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if sockerr == nil {
			sockerr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	})
	if err != nil {
		return err
	}
	return sockerr
}
