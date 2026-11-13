package connectivity

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func TestTCPSTUNFramingMappingAndShutdown(t *testing.T) {
	s, err := ListenTCPSTUN("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	// Fragment the first message and coalesce two others into a single write.
	for _, b := range request.Raw {
		if _, err = c.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	response, err := readSTUNStream(c)
	if err != nil || response.TransactionID != request.TransactionID {
		t.Fatal("fragmented binding", err)
	}
	var mapped stun.XORMappedAddress
	if err = mapped.GetFrom(response); err != nil {
		t.Fatal(err)
	}
	local := c.LocalAddr().(*net.TCPAddr)
	if mapped.Port != local.Port || !mapped.IP.Equal(local.IP) {
		t.Fatal("wrong TCP mapping", mapped, local)
	}
	if err = writeAll(c, append(append([]byte{}, request.Raw...), request.Raw...)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = readSTUNStream(c); err != nil {
			t.Fatal("coalesced binding", err)
		}
	}
	for range 2 {
		addr, err := TCPBinding(context.Background(), c)
		if err != nil || addr.String() != local.String() {
			t.Fatal("persistent mapping", addr, err)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = c.Read(make([]byte, 1)); err == nil {
		t.Fatal("shutdown left active TCP probes open")
	}
}

func TestTCPSTUNRejectsOversizedStream(t *testing.T) {
	s, err := ListenTCPSTUN("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	binary.BigEndian.PutUint16(request.Raw[2:4], 65532)
	_, _ = c.Write(request.Raw)
	if _, err = c.Read(make([]byte, 1)); err == nil {
		t.Fatal("unbounded stream frame accepted")
	}
}

func TestTCPProbeAndPeerDialReuseSourcePort(t *testing.T) {
	for _, family := range []struct{ network, host string }{
		{"tcp4", "127.0.0.1"}, {"tcp6", "::1"},
	} {
		t.Run(family.network, func(t *testing.T) {
			testTCPProbeAndPeerDialReuseSourcePort(t, family.network, family.host)
		})
	}
}
func testTCPProbeAndPeerDialReuseSourcePort(t *testing.T, network, host string) {
	s, err := ListenTCPSTUN(net.JoinHostPort(host, "0"))
	if err != nil {
		if network == "tcp6" {
			t.Skipf("IPv6 unavailable: %v", err)
		}
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	candidates := make(chan TCPCandidate, 4)
	a, err := NewTCPAgent(ctx, func(c TCPCandidate) {
		if c.Kind == "srflx" {
			candidates <- c
		}
	}, func(c net.Conn) { c.Close() })
	if err != nil {
		t.Fatal(err)
	}
	a.Start([]string{s.Addr().String()})
	defer func() { a.Close(); a.Wait() }()
	var mapped TCPCandidate
	select {
	case mapped = <-candidates:
	case <-ctx.Done():
		t.Fatal("TCP mapping not discovered")
	}
	listener, err := net.Listen(network, net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var socket *tcpSocket
	for _, candidate := range a.sockets {
		if candidate.network == network {
			socket = candidate
		}
	}
	if socket == nil {
		t.Fatal("no reusable socket", network)
	}
	c, err := a.dial(ctx, socket, listener.Addr().String())
	if err != nil {
		t.Fatal("port reuse failed", err)
	}
	defer c.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if c.LocalAddr().(*net.TCPAddr).Port != socket.local.Port || mapped.Address != peer.RemoteAddr().String() {
		t.Fatal("TCP STUN and peer used different mappings", mapped, c.LocalAddr(), peer.RemoteAddr())
	}
}
