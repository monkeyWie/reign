//go:build !windows && !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package codexipc

import (
	"context"
	"errors"
	"net"
)

func Endpoint(string) string { return "" }
func dialLocal(context.Context, string) (net.Conn, error) {
	return nil, errors.New("local Codex IPC is not supported on this platform")
}
func WriterLocked(string) (bool, error) {
	return true, errors.New("writer lock probing is not supported on this platform")
}
