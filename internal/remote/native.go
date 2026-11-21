package remote

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/pion/ice/v4"
	"github.com/pion/stun/v3"
	"github.com/quic-go/quic-go"
)

type nativeState struct {
	direct, relay   atomic.Pointer[nativepath.Link]
	welcome         *relay.NativeWelcome
	hostID          string
	latestTicket    atomic.Pointer[string]
	negotiated      atomic.Bool
	quicEnabled     atomic.Bool
	relayNegotiated atomic.Bool
	mu              sync.Mutex
	agent           *ice.Agent
	init            bool
	credentials     chan quicSignal
}
type quicSignal struct {
	Type      string `json:"type"`
	User      string `json:"user,omitempty"`
	Password  string `json:"password,omitempty"`
	Candidate string `json:"quicCandidate,omitempty"`
}
type icePackets struct {
	*ice.Conn
	remote net.Addr
}

func (p icePackets) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.Read(b)
	return n, p.remote, err
}
func (p icePackets) WriteTo(b []byte, _ net.Addr) (int, error) { return p.Write(b) }

func (w *wire) startQUIC(s quicSignal) {
	if !w.native.quicEnabled.Load() || w.opts.RelayOnly || w.ctx.Err() != nil {
		return
	}
	n := &w.native
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.init {
		n.init = true
		var urls []*stun.URI
		for _, server := range connectivity.STUNOnly(w.opts.ICEServers) {
			for _, url := range server.URLs {
				if parsed, err := stun.ParseURI(url); err == nil {
					urls = append(urls, parsed)
				}
			}
		}
		a, err := ice.NewAgent(&ice.AgentConfig{Urls: urls, NetworkTypes: []ice.NetworkType{ice.NetworkTypeUDP4, ice.NetworkTypeUDP6}, MulticastDNSMode: ice.MulticastDNSModeDisabled})
		if err != nil {
			return
		}
		n.agent = a
		n.credentials = make(chan quicSignal, 1)
		go w.runQUIC(a, n.credentials)
	}
	if n.agent == nil {
		return
	}
	if s.Candidate != "" && len(s.Candidate) <= 2048 {
		if c, err := ice.UnmarshalCandidate(s.Candidate); err == nil {
			_ = n.agent.AddRemoteCandidate(c)
		}
	}
	if s.User != "" && s.Password != "" {
		select {
		case n.credentials <- s:
		default:
		}
	}
}

func (w *wire) runQUIC(a *ice.Agent, credentials <-chan quicSignal) {
	closeICE := sync.OnceFunc(func() { _ = a.Close() })
	defer closeICE()
	stop := context.AfterFunc(w.ctx, closeICE)
	defer stop()
	setup, cancel := context.WithTimeout(w.ctx, 15*time.Second)
	defer cancel()
	send := func(s quicSignal) error { raw, _ := json.Marshal(s); return w.relaySendContext(setup, "signal", raw) }
	user, pwd, err := a.GetLocalUserCredentials()
	if err != nil || send(quicSignal{Type: "native-quic", User: user, Password: pwd}) != nil {
		return
	}
	if err = a.OnCandidate(func(c ice.Candidate) {
		if c != nil && setup.Err() == nil {
			_ = send(quicSignal{Type: "native-quic", Candidate: c.Marshal()})
		}
	}); err != nil {
		return
	}
	if a.GatherCandidates() != nil {
		return
	}
	var s quicSignal
	select {
	case s = <-credentials:
	case <-setup.Done():
		return
	}
	var conn *ice.Conn
	if w.host {
		conn, err = a.Accept(setup, s.User, s.Password)
	} else {
		conn, err = a.Dial(setup, s.User, s.Password)
	}
	if err != nil {
		return
	}
	remote := conn.RemoteAddr()
	if remote == nil {
		return
	}
	t := &quic.Transport{Conn: icePackets{conn, remote}}
	// Close the ICE socket before waiting for QUIC's packet reader to exit.
	defer func() { closeICE(); _ = t.Close() }()
	serverKey := nativepath.Key(w.tcpKey[:], "direct-quic/host")
	clientKey := nativepath.Key(w.tcpKey[:], "direct-quic/controller")
	clientCert, err := nativepath.ServerTLS(clientKey)
	if err != nil {
		return
	}
	var c *quic.Conn
	if w.host {
		cfg, err := nativepath.ServerTLS(serverKey)
		if err != nil {
			return
		}
		cfg.ClientAuth = tls.RequireAnyClientCert
		cfg.VerifyConnection = nativepath.ClientTLS(nativepath.Pin(clientKey)).VerifyConnection
		listener, err := t.Listen(cfg, nativepath.QUICConfig())
		if err != nil {
			return
		}
		defer listener.Close()
		c, err = listener.Accept(setup)
		if err != nil {
			return
		}
	} else {
		cfg := nativepath.ClientTLS(nativepath.Pin(serverKey))
		cfg.Certificates = clientCert.Certificates
		c, err = t.Dial(setup, remote, cfg, nativepath.QUICConfig())
		if err != nil {
			return
		}
	}
	l, err := nativepath.QUICLink(setup, c, w.host)
	if err != nil {
		return
	}
	cancel()
	w.useNative(l, &w.native.direct, "P2P · QUIC")
}

func (w *wire) refreshNativeTicket(info *relay.NativeWelcome) {
	if info != nil && w.native.welcome != nil && info.Endpoint == w.native.welcome.Endpoint && relay.IsExternalTicket(info.Ticket) {
		w.native.latestTicket.Store(&info.Ticket)
	}
}
func (w *wire) runNativeRelay() {
	info := w.native.welcome
	if info == nil {
		return
	}
	role := "client"
	if w.host {
		role = "host"
	}
	join := relay.NativeJoin{Host: w.native.hostID, Role: role, Peer: w.peer, Ticket: info.Ticket}
	for w.ctx.Err() == nil && !w.signalLost.Load() {
		for _, network := range []string{"QUIC", "TCP"} {
			if info.AddressFor(network) == "" || network == "QUIC" && w.opts.DisableQUIC {
				continue
			}
			if w.ctx.Err() != nil || w.signalLost.Load() {
				return
			}
			w.connectNativeRelay(info.Endpoint, network, join)
		}
		select {
		case <-w.ctx.Done():
			return
		case <-time.After(8 * time.Second):
		}
	}
}

func (w *wire) hasNativeRelay() bool {
	e := w.native.welcome
	return e != nil && (e.AddressFor("TCP") != "" || !w.opts.DisableQUIC && e.AddressFor("QUIC") != "")
}

func (w *wire) connectNativeRelay(endpoint nativepath.Endpoint, network string, join relay.NativeJoin) {
	dial, stop := context.WithTimeout(w.ctx, 3*time.Second)
	l, err := nativepath.Dial(dial, endpoint, network)
	stop()
	if err != nil {
		return
	}
	defer l.Close()
	stopClose := context.AfterFunc(w.ctx, l.Close)
	defer stopClose()
	setup, cancel := context.WithTimeout(w.ctx, 12*time.Second)
	if ticket := w.native.latestTicket.Load(); ticket != nil {
		join.Ticket = *ticket
	}
	raw, _ := json.Marshal(join)
	err = l.Send(setup, 0, raw)
	if err == nil {
		var ack []byte
		ack, err = l.Setup(setup)
		if err == nil && string(ack) != "ready" {
			err = errors.New("invalid native relay welcome")
		}
	}
	cancel()
	if err == nil {
		if relay.IsExternalTicket(join.Ticket) {
			life, end := context.WithCancel(w.ctx)
			defer end()
			go func(last string) {
				tick := time.NewTicker(3 * time.Second)
				defer tick.Stop()
				for {
					select {
					case <-life.Done():
						return
					case <-tick.C:
						if next := w.native.latestTicket.Load(); next != nil && *next != last {
							if l.Send(life, nativepath.LeaseLane, []byte(*next)) != nil {
								l.Close()
								return
							}
							last = *next
						}
					}
				}
			}(join.Ticket)
		}
		w.useNative(l, &w.native.relay, "Relay · "+network)
	}
}

func (w *wire) useNative(l *nativepath.Link, slot *atomic.Pointer[nativepath.Link], name string) {
	stop := context.AfterFunc(w.ctx, l.Close)
	defer stop()
	slot.Store(l)
	defer slot.CompareAndSwap(l, nil)
	w.progress("path:"+name, "transport", name, "已建立端到端加密数据通道", "success")
	l.Receive(func(_ byte, b []byte) error { w.onChunk(b); return w.ctx.Err() })
}
