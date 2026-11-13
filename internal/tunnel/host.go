package tunnel

import (
	"context"
	"crypto/subtle"
	"errors"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/relay"
	"github.com/monkeyWie/reign/internal/remote"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Host struct {
	URL    string
	Relay  *relay.Server
	Status *remote.HostStatus
	close  func()
	once   sync.Once
}

// Listen creates a loopback-only transport endpoint. Public routes never expose
// the directory, settings, files, or a plaintext Agent API.
func Listen(ctx context.Context, listen, hostID string, opts remote.Options, d *control.Dispatcher, authorize func(string) (string, bool)) (*Host, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		ln.Close()
		return nil, errors.New("tunnel origin must listen on loopback")
	}
	token := device.Random()
	r := relay.New("")
	// Browsers have unrelated origins. Possession of a per-device tunnel token
	// and the independent encrypted handshake authorize each connection.
	r.Origins = []string{"*"}
	r.Authorize = func(credential, target, role string) (string, bool) {
		if target != hostID {
			return "", false
		}
		if role == "host" {
			return hostID, subtle.ConstantTimeCompare([]byte(credential), []byte(token)) == 1
		}
		if role != "client" {
			return "", false
		}
		return authorize(credential)
	}
	life, cancel := context.WithCancel(ctx)
	url := "http://" + ln.Addr().String()
	h := &Host{URL: url, Relay: r, Status: remote.NewHostStatus("Cloudflare Tunnel")}
	server := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	h.close = func() { cancel(); _ = server.Close(); h.Status.DisconnectAll() }
	go func() { _ = server.Serve(ln) }()
	opts.RelayName = "Cloudflare Tunnel"
	go remote.RunHostObserved(life, remote.Pairing{Relay: strings.Replace(url, "http", "ws", 1), Host: hostID, Key: device.Random(), RelayToken: token}, opts, d, h.Status)
	go func() { <-life.Done(); h.Close() }()
	return h, nil
}
func (h *Host) Close() { h.once.Do(h.close) }
