package connectivity

import (
	"golang.org/x/sys/windows"
	"syscall"
)

func reuseTCPPort(_, _ string, raw syscall.RawConn) error {
	var sockerr error
	err := raw.Control(func(fd uintptr) {
		sockerr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return sockerr
}
