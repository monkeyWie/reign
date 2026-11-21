package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/quic-go/quic-go"
)

// The welcome ticket only attaches data sockets to this signal generation.
type NativeWelcome struct {
	nativepath.Endpoint
	Ticket string `json:"ticket"`
}
type NativeJoin struct {
	Host   string `json:"host"`
	Peer   string `json:"peer"` // controller's signal peer, for both roles
	Role   string `json:"role"`
	Ticket string `json:"ticket"`
}
type nativePair struct {
	tickets  [2]string
	admitted chan struct{}
	legs     [2]*nativepath.Link
	ready    chan struct{}
	ack      [2]chan struct{}
	done     chan struct{}
	once     sync.Once
}
type NativeListener struct {
	tcp    net.Listener
	udp    *quic.Listener
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

func (n *NativeListener) Close() {
	n.once.Do(func() {
		n.cancel()
		_ = n.tcp.Close()
		_ = n.udp.Close()
		n.wg.Wait()
	})
}
func (n *NativeListener) Addr() net.Addr { return n.tcp.Addr() }

// ListenNative starts both data protocols on one numeric port. Call before
// serving signaling. External admission uses the configured control origin.
func (s *Server) ListenNative(address, public string, key ed25519.PrivateKey) (*NativeListener, error) {
	var authority *externalAuthority
	if s.ControlOrigin != "" {
		if !ValidControlOrigin(s.ControlOrigin) {
			return nil, errors.New("invalid relay control origin")
		}
		authority = &externalAuthority{origin: s.ControlOrigin, key: key, pin: nativepath.Pin(key), client: http.Client{
			Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}}
	}
	cfg, err := nativepath.ServerTLS(key)
	if err != nil {
		return nil, err
	}
	host, requestedPort, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var tcp net.Listener
	var udp *quic.Listener
	var port string
	for attempt := 0; ; attempt++ {
		tcp, err = net.Listen("tcp", address)
		if err != nil {
			return nil, err
		}
		_, port, _ = net.SplitHostPort(tcp.Addr().String())
		udp, err = quic.ListenAddr(net.JoinHostPort(host, port), cfg, nativepath.QUICConfig())
		if err == nil {
			break
		}
		_ = tcp.Close()
		// TCP's ephemeral allocator cannot see already-bound UDP ports.
		if requestedPort != "0" || attempt >= 7 || !errors.Is(err, syscall.EADDRINUSE) {
			return nil, err
		}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	n := &NativeListener{tcp: tcp, udp: udp, cancel: cancel}
	s.nativePublic, s.nativePort, s.nativePin = public, port, nativepath.Pin(key)
	slots := make(chan struct{}, 512)
	start := func(serve func(), reject func()) {
		select {
		case slots <- struct{}{}:
			n.wg.Go(func() {
				defer func() { <-slots }()
				serve()
			})
		default:
			reject()
		}
	}
	n.wg.Go(func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			start(func() {
				conn := tls.Server(c, cfg)
				defer c.Close()
				setup, cancel := context.WithTimeout(ctx, 8*time.Second)
				err := conn.HandshakeContext(setup)
				cancel()
				if err == nil && conn.ConnectionState().NegotiatedProtocol == nativepath.ALPN {
					s.serveNative(ctx, nativepath.TCPLink(conn), authority)
				}
			}, func() { _ = c.Close() })
		}
	})
	n.wg.Go(func() {
		for {
			c, err := udp.Accept(ctx)
			if err != nil {
				return
			}
			start(func() {
				setup, cancel := context.WithTimeout(ctx, 8*time.Second)
				l, err := nativepath.QUICLink(setup, c, true)
				cancel()
				if err == nil {
					s.serveNative(ctx, l, authority)
				}
			}, func() { _ = c.CloseWithError(1, "busy") })
		}
	})
	return n, nil
}

func (s *Server) serveNative(ctx context.Context, l *nativepath.Link, authority *externalAuthority) {
	defer l.Close()
	stop := context.AfterFunc(ctx, l.Close)
	defer stop()
	setup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, err := l.Setup(setup)
	var join NativeJoin
	if err != nil || len(raw) > 1024 || json.Unmarshal(raw, &join) != nil || join.Ticket == "" {
		return
	}
	idx := 0
	if join.Role == "host" {
		idx = 1
	} else if join.Role != "client" {
		return
	}
	external := strings.HasPrefix(join.Ticket, "ext1.")
	key := [2]string{join.Host, join.Peer}
	var pair *nativePair
	var client *connection
	if external && (authority == nil || !idPattern.MatchString(join.Host) || !idPattern.MatchString(join.Peer) || !ticketPattern.MatchString(join.Ticket)) {
		return
	}

	s.mu.Lock()
	if external {
		pair = s.externalPairs[key]
	} else {
		room := s.rooms[join.Host]
		if room == nil || room.clients[join.Peer] == nil {
			s.mu.Unlock()
			return
		}
		client = room.clients[join.Peer]
		member := client
		if idx == 1 {
			member = room.host
		}
		if client.nativePin != s.nativePin || room.host.nativePin != s.nativePin ||
			subtle.ConstantTimeCompare([]byte(member.nativeTicket), []byte(join.Ticket)) != 1 {
			s.mu.Unlock()
			return
		}
		stopHost := context.AfterFunc(room.host.life, l.Close)
		stopClient := context.AfterFunc(client.life, l.Close)
		defer stopHost()
		defer stopClient()
		pair = client.native
	}
	if pair == nil {
		pair = &nativePair{ready: make(chan struct{}), admitted: make(chan struct{}), ack: [2]chan struct{}{make(chan struct{}), make(chan struct{})}, done: make(chan struct{})}
		if external {
			s.externalPairs[key] = pair
		} else {
			client.native = pair
		}
	}
	if pair.legs[idx] != nil {
		s.mu.Unlock()
		return
	}
	pair.legs[idx] = l
	pair.tickets[idx] = join.Ticket
	if pair.legs[1-idx] != nil {
		close(pair.ready)
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		pair.once.Do(func() { close(pair.done) })
		if external {
			if s.externalPairs[key] == pair {
				delete(s.externalPairs, key)
			}
		} else if client.native == pair {
			client.native = nil
		}
		other := pair.legs[1-idx]
		s.mu.Unlock()
		if other != nil {
			other.Close()
		}
	}()
	select {
	case <-setup.Done():
		return
	case <-pair.done:
		return
	case <-pair.ready:
	}
	if external {
		if idx == 0 {
			check := leaseCheck{Host: join.Host, Peer: join.Peer, HostTicket: pair.tickets[1], ClientTicket: pair.tickets[0]}
			until, err := authority.lease(setup, check)
			if err != nil || !until.After(time.Now()) {
				return
			}
			life, end := context.WithCancel(ctx)
			defer end()
			go authority.maintain(life, check, until, l.Close)
			close(pair.admitted)
		} else {
			select {
			case <-setup.Done():
				return
			case <-pair.done:
				return
			case <-pair.admitted:
			}
		}
	}
	other := pair.legs[1-idx]
	// Forward only after both ready messages have been written, otherwise the
	// peer's first RPC could arrive in place of the admission acknowledgement.
	if l.Send(setup, 0, []byte("ready")) != nil {
		return
	}
	close(pair.ack[idx])
	select {
	case <-pair.ack[1-idx]:
	case <-pair.done:
		return
	case <-setup.Done():
		return
	}
	l.Receive(func(lane byte, body []byte) error {
		return other.Send(ctx, lane, body)
	})
}
