package codexipc

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func Endpoint(string) string { return `\\.\pipe\codex-ipc` }
func dialLocal(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
