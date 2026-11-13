//go:build !unix && !windows

package connectivity

import (
	"errors"
	"syscall"
)

func reuseTCPPort(_, _ string, _ syscall.RawConn) error {
	return errors.New("TCP port reuse is unavailable on this platform")
}
