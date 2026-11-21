package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
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
	generation string
	legs       [2]*nativepath.Link
	ready      chan struct{}
	ack        [2]chan struct{}
	done       chan struct{}
	once       sync.Once
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
// serving signaling. External admission verifies tickets locally with the node key.
func (s *Server) ListenNative(address, public string, key ed25519.PrivateKey) (*NativeListener, error) {
	secret := authorizationKey(key)
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
					s.serveNative(ctx, nativepath.TCPLink(conn), secret)
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
					s.serveNative(ctx, l, secret)
				}
			}, func() { _ = c.CloseWithError(1, "busy") })
		}
	})
	return n, nil
}

func (s *Server) serveNative(ctx context.Context, l *nativepath.Link, secret []byte) {
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
	external := IsExternalTicket(join.Ticket)
	key := [2]string{join.Host, join.Peer}
	var pair *nativePair
	var client *connection
	var generation string
	var expiry time.Time
	if external {
		var valid bool
		generation, expiry, valid = verifyTicket(secret, join, time.Now())
		if !valid {
			return
		}
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
		pair = &nativePair{ready: make(chan struct{}), generation: generation, ack: [2]chan struct{}{make(chan struct{}), make(chan struct{})}, done: make(chan struct{})}
		if external {
			s.externalPairs[key] = pair
		} else {
			client.native = pair
		}
	}
	if pair.generation != generation || pair.legs[idx] != nil {
		s.mu.Unlock()
		return
	}
	pair.legs[idx] = l
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
	if external && !time.Now().Before(expiry) {
		return
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
	// Each leg must keep its own authorization current. Link.Close interrupts
	// blocked reads and writes even while the connection is otherwise idle.
	var expiryTimer *time.Timer
	if external {
		expiryTimer = time.AfterFunc(time.Until(expiry), l.Close)
		defer expiryTimer.Stop()
	}
	l.Receive(func(lane byte, body []byte) error {
		if lane == nativepath.LeaseLane {
			if !external || len(body) > 512 || !time.Now().Before(expiry) {
				return errors.New("unexpected native lease")
			}
			join.Ticket = string(body)
			gen, next, valid := verifyTicket(secret, join, time.Now())
			if !valid || gen != generation {
				return errors.New("invalid native lease")
			}
			if next.After(expiry) {
				if !expiryTimer.Stop() {
					return errors.New("native lease expired")
				}
				expiry = next
				expiryTimer.Reset(time.Until(expiry))
			}
			return nil
		}
		return other.Send(ctx, lane, body)
	})
}
