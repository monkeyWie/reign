package remote

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTCPMutualAuthenticationRejectsWrongKeyAndRole(t *testing.T) {
	for _, tc := range []struct {
		name                string
		wrongKey, wrongRole bool
	}{
		{"valid", false, false}, {"wrong-key", true, false}, {"reflection-role", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			client, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			host, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			_ = host.SetDeadline(time.Now().Add(time.Second))
			_ = client.SetDeadline(time.Now().Add(time.Second))
			key := [32]byte{1, 2, 3}
			other := key
			if tc.wrongKey {
				other[0]++
			}
			done := make(chan error, 1)
			go func() { done <- tcpAuthenticate(host, key, !tc.wrongRole) }()
			clientErr := tcpAuthenticate(client, other, false)
			hostErr := <-done
			wantOK := !tc.wrongKey && !tc.wrongRole
			if (clientErr == nil) != wantOK || (hostErr == nil) != wantOK {
				t.Fatal("invalid authentication outcome", hostErr, clientErr)
			}
		})
	}
}

// Observe the host waiting for USE1 on a socket the controller did not choose.
type selectionReadConn struct {
	net.Conn
	waiting chan struct{}
	once    sync.Once
}

func (c *selectionReadConn) Read(b []byte) (int, error) {
	if len(b) == 4 {
		c.once.Do(func() { close(c.waiting) })
	}
	return c.Conn.Read(b)
}

func TestTCPCrossingDialsDoNotBlockControllersChosenSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	pair := func() (net.Conn, net.Conn) {
		client, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		host, err := listener.Accept()
		if err != nil {
			client.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close(); host.Close() })
		client.SetDeadline(time.Now().Add(3 * time.Second))
		return client, host
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key := [32]byte{1, 2, 3}
	w := &wire{ctx: ctx, host: true, tcpKey: key}
	defer w.closeTCP()
	first, hostFirst := pair()
	waiting := make(chan struct{})
	go w.onTCPConn(&selectionReadConn{Conn: hostFirst, waiting: waiting})
	if err := tcpAuthenticate(first, key, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("first candidate did not finish authentication")
	}
	// Choose the other crossing dial while the first waits for a choice forever.
	chosen, hostChosen := pair()
	go w.onTCPConn(hostChosen)
	if err := tcpAuthenticate(chosen, key, false); err != nil {
		t.Fatal(err)
	}
	if err := tcpWriteAll(chosen, []byte("USE1")); err != nil {
		t.Fatal(err)
	}
	ack := make([]byte, 4)
	if _, err := io.ReadFull(chosen, ack); err != nil || string(ack) != "ACK1" {
		t.Fatalf("unchosen socket blocked selection of chosen socket: %q %v", ack, err)
	}
	waitFor(t, func() bool { return w.tcpSnapshot() != nil })
	if w.tcpSnapshot().conn != hostChosen {
		t.Fatal("host retained the unchosen crossing dial")
	}
}
