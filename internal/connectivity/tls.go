package connectivity

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http/httptrace"
	"time"
)

// net.Dialer's Happy Eyeballs chooses a winner after TCP. A route can accept
// TCP while silently dropping TLS, so include the authenticated TLS handshake
// in the race. Only one winner proceeds to HTTP or WebSocket authorization.
func dialTLSWithFallback(ctx context.Context, network, address string, config *tls.Config) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if config == nil {
		config = &tls.Config{}
	} else {
		config = config.Clone()
	}
	if config.ServerName == "" {
		config.ServerName = host
	}
	dial := func(family string) func(context.Context) (net.Conn, error) {
		return func(ctx context.Context) (net.Conn, error) {
			return dialTLSFamily(ctx, family, address, config)
		}
	}
	if network != "tcp" || net.ParseIP(host) != nil {
		return dial(network)(ctx)
	}
	return raceTLS(ctx, 300*time.Millisecond, dial("tcp6"), dial("tcp4"))
}

func dialTLSFamily(ctx context.Context, network, address string, config *tls.Config) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: StageTimeout, KeepAlive: 30 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, config)
	trace := httptrace.ContextClientTrace(ctx)
	if trace != nil && trace.TLSHandshakeStart != nil {
		trace.TLSHandshakeStart()
	}
	err = conn.HandshakeContext(ctx)
	if trace != nil && trace.TLSHandshakeDone != nil {
		trace.TLSHandshakeDone(conn.ConnectionState(), err)
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func raceTLS(ctx context.Context, delay time.Duration, primary, fallback func(context.Context) (net.Conn, error)) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result)
	start := func(dial func(context.Context) (net.Conn, error)) {
		go func() {
			conn, err := dial(life)
			select {
			case results <- result{conn, err}:
			case <-life.Done():
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()
	}
	start(primary)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	startedFallback := false
	var failures []error
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			start(fallback)
			startedFallback = true
		case r := <-results:
			if r.err == nil {
				return r.conn, nil
			}
			failures = append(failures, r.err)
			if len(failures) == 2 {
				return nil, errors.Join(failures...)
			}
			if !startedFallback {
				timer.Stop()
				start(fallback)
				startedFallback = true
			}
		}
	}
}
