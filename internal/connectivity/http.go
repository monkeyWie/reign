package connectivity

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"time"
)

// ConnectionHTTPClient keeps TCP and TLS setup within the same per-stage
// budget as HTTP and WebSocket setup, instead of Go's shorter default limits.
// WebSocket lifetime is owned by its context after the upgrade succeeds.
var ConnectionHTTPClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if runtime.GOOS == "js" {
		// WebAssembly must use the browser Fetch transport, not a TCP dialer.
		transport.DialContext = nil
	} else {
		transport.DialContext = (&net.Dialer{Timeout: StageTimeout, KeepAlive: 30 * time.Second}).DialContext
		transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialTLSWithFallback(ctx, network, address, transport.TLSClientConfig)
		}
	}
	transport.TLSHandshakeTimeout = StageTimeout
	return &http.Client{Timeout: StageTimeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}()
