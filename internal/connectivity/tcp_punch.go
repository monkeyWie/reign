package connectivity

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/wlynxg/anet"
)

// TCPCandidate is exchanged over the authenticated, encrypted signaling link.
// The STUN transport is configured separately from WebRTC's UDP STUN URLs.
type TCPCandidate struct {
	Address string `json:"address"`
	Kind    string `json:"kind"` // host or srflx
}
type tcpSocket struct {
	listener net.Listener
	network  string
	local    *net.TCPAddr
}

// TCPAgent listens, probes TCP mappings and actively dials the peer from the
// SAME source port. Both ends do this concurrently (TCP simultaneous open).
// Endpoint-dependent NATs and firewalls can still reject it; relay stays live.
// Accepted/dialed connections must be authenticated by onConn before use.
type TCPAgent struct {
	Progress    func(Progress)
	Trace       func(string, ...any)
	ctx         context.Context
	cancel      context.CancelFunc
	sockets     []*tcpSocket
	onCandidate func(TCPCandidate)
	onConn      func(net.Conn)
	mu          sync.Mutex
	remote      map[string]bool
	local       map[string]bool
	slots       chan struct{}
	wg          sync.WaitGroup
	startOnce   sync.Once
	closeOnce   sync.Once
	stopped     bool
}

func NewTCPAgent(ctx context.Context, onCandidate func(TCPCandidate), onConn func(net.Conn)) (*TCPAgent, error) {
	life, cancel := context.WithTimeout(ctx, StageTimeout)
	a := &TCPAgent{ctx: life, cancel: cancel, onCandidate: onCandidate, onConn: onConn,
		remote: make(map[string]bool), local: make(map[string]bool), slots: make(chan struct{}, 16)}
	for _, family := range []string{"tcp4", "tcp6"} {
		l, err := (&net.ListenConfig{Control: reuseTCPPort}).Listen(life, family, ":0")
		if err != nil {
			continue
		}
		addr := l.Addr().(*net.TCPAddr)
		a.sockets = append(a.sockets, &tcpSocket{listener: l, network: family, local: addr})
	}
	if len(a.sockets) == 0 {
		cancel()
		return nil, errors.New("cannot create reusable TCP listeners")
	}
	return a, nil
}

func (a *TCPAgent) Start(servers []string) {
	a.startOnce.Do(func() {
		// Start owns worker registration; Close may run at any time.
		for _, s := range a.sockets {
			a.launch(func() { a.accept(s) })
		}
		a.launch(func() {
			addresses, err := anet.InterfaceAddrs()
			if err != nil {
				a.progress("tcp-interfaces", "无法读取本机 TCP 候选地址", err.Error(), "warning")
				return
			}
			for _, s := range a.sockets {
				for _, address := range addresses {
					ip, _, err := net.ParseCIDR(address.String())
					if err != nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
						continue
					}
					if (ip.To4() != nil) != (s.network == "tcp4") {
						continue
					}
					a.publish(TCPCandidate{Address: net.JoinHostPort(ip.String(), strconv.Itoa(s.local.Port)), Kind: "host"})
				}
			}
		})
		seen := map[string]bool{}
		for _, server := range servers {
			if seen[server] || len(seen) >= 4 {
				continue
			}
			seen[server] = true
			if _, _, err := net.SplitHostPort(server); err != nil {
				continue
			}
			for _, s := range a.sockets {
				a.launch(func() { a.probe(s, server) })
			}
		}
		go func() {
			<-a.ctx.Done()
			for _, s := range a.sockets {
				_ = s.listener.Close()
			}
		}()
	})
}
func (a *TCPAgent) Close() {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.stopped = true
		a.mu.Unlock()
		a.cancel()
		for _, s := range a.sockets {
			_ = s.listener.Close()
		}
	})
}

// Register workers under the shutdown lock, including callers outside existing
// workers (AddRemote). No Add can race a zero-count Wait after Close.
func (a *TCPAgent) launch(fn func()) bool {
	a.mu.Lock()
	if a.stopped || a.ctx.Err() != nil {
		a.mu.Unlock()
		return false
	}
	a.wg.Add(1)
	a.mu.Unlock()
	go func() { defer a.wg.Done(); fn() }()
	return true
}

// Wait is for shutdown verification, and must be called after Start returns.
func (a *TCPAgent) Wait() { a.wg.Wait() }

func (a *TCPAgent) publish(c TCPCandidate) {
	a.mu.Lock()
	if a.local[c.Address] || len(a.local) >= 32 || a.ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	a.local[c.Address] = true
	a.mu.Unlock()
	if a.Trace != nil {
		a.Trace("TCP local %s %s", c.Kind, c.Address)
	}
	a.onCandidate(c)
}
func (a *TCPAgent) accept(s *tcpSocket) {
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		if a.Trace != nil {
			a.Trace("TCP accepted %s <- %s", c.LocalAddr(), c.RemoteAddr())
		}
		select {
		case a.slots <- struct{}{}:
			if !a.launch(func() { defer func() { <-a.slots }(); a.onConn(c) }) {
				<-a.slots
				_ = c.Close()
			}
		default:
			_ = c.Close()
		}
	}
}
func (a *TCPAgent) dial(ctx context.Context, s *tcpSocket, address string) (net.Conn, error) {
	d := net.Dialer{LocalAddr: s.local, Control: reuseTCPPort, Timeout: 2 * time.Second, KeepAlive: 15 * time.Second}
	return d.DialContext(ctx, s.network, address)
}
func (a *TCPAgent) probe(s *tcpSocket, server string) {
	a.progress("tcp-stun:"+s.network+server, "探测 TCP 公网映射", server+" · "+s.network, "running")
	d := net.Dialer{LocalAddr: s.local, Control: reuseTCPPort, Timeout: StageTimeout, KeepAlive: 15 * time.Second}
	c, err := d.DialContext(a.ctx, s.network, server)
	if err != nil {
		if a.Trace != nil {
			a.Trace("TCP STUN %s %s: %v", s.network, server, err)
		}
		a.progress("tcp-stun:"+s.network+server, "TCP 探测节点未响应", server+" · "+s.network+" · "+err.Error(), "warning")
		return
	}
	defer c.Close()
	stop := context.AfterFunc(a.ctx, func() { _ = c.Close() })
	defer stop()
	for {
		mapped, err := TCPBinding(a.ctx, c)
		if err != nil {
			if a.Trace != nil {
				a.Trace("TCP binding %s: %v", server, err)
			}
			a.progress("tcp-stun:"+s.network+server, "TCP 公网映射探测结束", server+" · "+err.Error(), "warning")
			return
		}
		a.progress("tcp-stun:"+s.network+server, "已获得 TCP 公网映射", server+" → "+mapped.String(), "success")
		a.publish(TCPCandidate{Address: mapped.String(), Kind: "srflx"})
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func ValidTCPCandidate(c TCPCandidate) bool {
	addr, err := netip.ParseAddrPort(c.Address)
	return err == nil && addr.Port() != 0 && !addr.Addr().IsUnspecified() && !addr.Addr().IsMulticast() &&
		!addr.Addr().IsLinkLocalUnicast() && addr.Addr().Zone() == "" && (c.Kind == "host" || c.Kind == "srflx")
}
func (a *TCPAgent) AddRemote(c TCPCandidate) {
	if !ValidTCPCandidate(c) {
		return
	}
	a.mu.Lock()
	if a.remote[c.Address] || len(a.remote) >= 32 || a.ctx.Err() != nil {
		a.mu.Unlock()
		return
	}
	a.remote[c.Address] = true
	a.mu.Unlock()
	if a.Trace != nil {
		a.Trace("TCP remote %s %s", c.Kind, c.Address)
	}
	address, _ := netip.ParseAddrPort(c.Address)
	for _, s := range a.sockets {
		if address.Addr().Unmap().Is4() != (s.network == "tcp4") {
			continue
		}
		a.launch(func() {
			// Each side dials while still accepting, so either inbound accept or
			// simultaneous-open completion can yield the authenticated channel.
			for attempt := 0; attempt < 64 && a.ctx.Err() == nil; attempt++ {
				c, err := a.dial(a.ctx, s, c.Address)
				if a.Trace != nil {
					a.Trace("TCP dial %s source=%s attempt=%d result=%v", s.network, s.local, attempt, err)
				}
				if err == nil {
					select {
					case a.slots <- struct{}{}:
						a.onConn(c)
						<-a.slots
					default:
						_ = c.Close()
					}
					return
				}
				select {
				case <-a.ctx.Done():
					return
				// Avoid repeating the same ordering when a router actively resets
				// unmatched SYNs. Bound all retries by the shared search deadline.
				case <-time.After(time.Duration(100+rand.IntN(200)) * time.Millisecond):
				}
			}
		})
	}
}

func (a *TCPAgent) progress(id, message, detail, state string) {
	if a.Progress != nil && a.ctx.Err() == nil {
		a.Progress(Progress{ID: id, Stage: "tcp", Message: message, Detail: detail, State: state})
	}
}
