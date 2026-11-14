package connectivity

import (
	"net"
	"net/http"
	"time"
)

// ConnectionHTTPClient keeps TCP and TLS setup within the same per-stage
// budget as HTTP and WebSocket setup, instead of Go's shorter default limits.
// WebSocket lifetime is owned by its context after the upgrade succeeds.
var ConnectionHTTPClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: StageTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = StageTimeout
	return &http.Client{Timeout: StageTimeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}()
