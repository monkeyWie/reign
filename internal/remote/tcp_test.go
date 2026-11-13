package remote

import (
	"net"
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
