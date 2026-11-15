package connectivity

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"runtime"
	"time"
)

// ConnectionHTTPClient keeps TCP and TLS setup within the same per-stage
// budget as HTTP and WebSocket setup, instead of Go's shorter default limits.
// WebSocket lifetime is owned by its context after the upgrade succeeds.
var ConnectionHTTPClient = newConnectionHTTPClient(false)

// RFC 6455 upgrades require HTTP/1.1. Keep their pool separate from ordinary
// HTTP/2 requests, including when a custom TLS dialer completes ALPN itself.
var ConnectionWebSocketClient = newConnectionHTTPClient(true)

func newConnectionHTTPClient(websocket bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if websocket {
		transport.Protocols = new(http.Protocols)
		transport.Protocols.SetHTTP1(true)
		transport.ForceAttemptHTTP2 = false
		// Clone can inherit h2 ALPN from an initialized DefaultTransport.
		// A custom TLS dialer bypasses net/http's per-request ALPN filtering.
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	}
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
}
