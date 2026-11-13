package connectivity

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pion/stun/v3"
)

// TCPSTUNServer implements RFC 8489 Binding over a TCP byte stream. TCP STUN
// discovers TCP mappings; UDP mappings cannot be used for TCP hole punching.
// It does not relay traffic or require an allocated port range.
type TCPSTUNServer struct {
	listener    net.Listener
	mu          sync.Mutex
	connections map[net.Conn]string
	perIP       map[string]int
	closed      bool
	wg          sync.WaitGroup
}

func ListenTCPSTUN(address string) (*TCPSTUNServer, error) {
	l, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	s := &TCPSTUNServer{listener: l, connections: make(map[net.Conn]string), perIP: make(map[string]int)}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *TCPSTUNServer) Addr() net.Addr { return s.listener.Addr() }
func (s *TCPSTUNServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	err := s.listener.Close()
	for c := range s.connections {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return err
}
func (s *TCPSTUNServer) accept() {
	defer s.wg.Done()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		s.mu.Lock()
		if s.closed || len(s.connections) >= 512 || s.perIP[ip] >= 32 {
			s.mu.Unlock()
			_ = c.Close()
			continue
		}
		s.connections[c] = ip
		s.perIP[ip]++
		s.wg.Add(1)
		s.mu.Unlock()
		go s.serve(c)
	}
}
func (s *TCPSTUNServer) serve(c net.Conn) {
	defer s.wg.Done()
	defer c.Close()
	defer func() {
		s.mu.Lock()
		ip := s.connections[c]
		delete(s.connections, c)
		s.perIP[ip]--
		if s.perIP[ip] == 0 {
			delete(s.perIP, ip)
		}
		s.mu.Unlock()
	}()
	addr := c.RemoteAddr().(*net.TCPAddr)
	window, count := time.Now(), 0
	for {
		_ = c.SetDeadline(time.Now().Add(45 * time.Second))
		request, err := readSTUNStream(c)
		if err != nil {
			return
		}
		if time.Since(window) >= time.Second {
			window, count = time.Now(), 0
		}
		count++
		if count > 20 {
			return
		}
		if request.Type != stun.BindingRequest {
			continue
		}
		if request.Contains(stun.AttrFingerprint) && stun.Fingerprint.Check(request) != nil {
			return
		}
		response, err := stun.Build(stun.NewTransactionIDSetter(request.TransactionID), stun.BindingSuccess,
			&stun.XORMappedAddress{IP: addr.IP, Port: addr.Port}, stun.Fingerprint)
		if err != nil || writeAll(c, response.Raw) != nil {
			return
		}
	}
}

// STUN has its own 20-byte header and length field on TCP (no extra RFC 4571
// prefix). ReadFull is essential: TCP can split or coalesce STUN messages.
func readSTUNStream(r io.Reader) (*stun.Message, error) {
	header := make([]byte, 20)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(header[2:4]))
	if header[0]&0xc0 != 0 || !stun.IsMessage(header) || n > 2028 || n%4 != 0 {
		return nil, errors.New("invalid TCP STUN frame")
	}
	raw := append(header, make([]byte, n)...)
	if _, err := io.ReadFull(r, raw[20:]); err != nil {
		return nil, err
	}
	m := stun.New()
	m.Raw = raw
	if err := m.Decode(); err != nil {
		return nil, err
	}
	return m, nil
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

// TCPBinding probes an already bound TCP socket, preserving its source port.
// The caller can keep the connection alive and reuse that port for peer dials.
func TCPBinding(ctx context.Context, c net.Conn) (*net.TCPAddr, error) {
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.SetDeadline(deadline)
	defer c.SetDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	if err := writeAll(c, request.Raw); err != nil {
		return nil, err
	}
	response, err := readSTUNStream(c)
	if err != nil {
		return nil, err
	}
	if response.Type != stun.BindingSuccess || response.TransactionID != request.TransactionID {
		return nil, errors.New("invalid TCP STUN response")
	}
	if response.Contains(stun.AttrFingerprint) && stun.Fingerprint.Check(response) != nil {
		return nil, errors.New("invalid TCP STUN fingerprint")
	}
	var mapped stun.XORMappedAddress
	if err := mapped.GetFrom(response); err != nil {
		return nil, err
	}
	if mapped.IP.IsUnspecified() || mapped.Port < 1 || mapped.Port > 65535 {
		return nil, errors.New("invalid TCP mapping")
	}
	return &net.TCPAddr{IP: mapped.IP, Port: mapped.Port}, nil
}
