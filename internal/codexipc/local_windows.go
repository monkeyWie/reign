package codexipc

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func Endpoint(string) string { return `\\.\pipe\codex-ipc` }
func dialLocal(ctx context.Context, path string) (net.Conn, error) {
	// GENERIC_WRITE also requests FILE_CREATE_PIPE_INSTANCE and attribute/EA
	// writes. A client only needs data exchange and pipe-info access; requesting
	// server rights can fail against a restricted DACL even for the same user.
	const access = windows.FILE_READ_DATA | windows.FILE_WRITE_DATA | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE
	return winio.DialPipeAccess(ctx, path, access)
}
