package connectivity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTLSFallbackEscapesAcceptedTCPWithStalledTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	stalled, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	primaryClosed := make(chan struct{})
	go func() {
		defer close(primaryClosed)
		conn, err := stalled.Accept()
		if err == nil {
			defer conn.Close()
			buffer := make([]byte, 4096)
			for {
				if _, err := conn.Read(buffer); err != nil {
					return
				}
			}
		}
	}()
	config := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	config.ServerName = "127.0.0.1"
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	started := time.Now()
	conn, err := raceTLS(ctx, 25*time.Millisecond,
		func(ctx context.Context) (net.Conn, error) {
			return dialTLSFamily(ctx, "tcp", stalled.Addr().String(), config)
		},
		func(ctx context.Context) (net.Conn, error) {
			return dialTLSFamily(ctx, "tcp", strings.TrimPrefix(server.URL, "https://"), config)
		},
	)
	if err != nil {
		t.Fatal("TLS fallback waited for the stalled route:", err)
	}
	defer conn.Close()
	if !conn.(*tls.Conn).ConnectionState().HandshakeComplete || time.Since(started) > 500*time.Millisecond {
		t.Fatal("winner chosen before TLS authentication or fallback started too late")
	}
	select {
	case <-primaryClosed:
	case <-ctx.Done():
		t.Fatal("losing TCP/TLS connection leaked")
	}
}

func TestTLSFallbackStillVerifiesCertificates(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	_, err := dialTLSWithFallback(ctx, "tcp", strings.TrimPrefix(server.URL, "https://"), nil)
	var invalid *tls.CertificateVerificationError
	if !errors.As(err, &invalid) {
		t.Fatalf("untrusted certificate accepted or error hidden: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	_, err = dialTLSWithFallback(ctx, "tcp", strings.TrimPrefix(server.URL, "https://"), &tls.Config{RootCAs: roots, ServerName: "wrong.example"})
	if !errors.As(err, &invalid) {
		t.Fatalf("TLS hostname verification bypassed: %v", err)
	}
}

func TestTLSFallbackKeepsFastPrimaryAndHonorsCancellation(t *testing.T) {
	first, remote := net.Pipe()
	defer first.Close()
	defer remote.Close()
	conn, err := raceTLS(context.Background(), time.Second,
		func(context.Context) (net.Conn, error) { return first, nil },
		func(context.Context) (net.Conn, error) {
			t.Error("fast primary opened redundant fallback")
			return nil, errors.New("unexpected")
		},
	)
	if err != nil || conn != first {
		t.Fatal(conn, err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	wait := func(ctx context.Context) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }
	_, err = raceTLS(ctx, time.Millisecond, wait, wait)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("TLS race ignored caller deadline:", err)
	}
}
