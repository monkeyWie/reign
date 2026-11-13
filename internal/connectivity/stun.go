package connectivity

import (
	"net"
	"sync"
	"time"

	"github.com/pion/stun/v3"
)

// STUNServer implements only RFC 5389 Binding over UDP. It neither allocates
// relay addresses nor forwards peer traffic, so no UDP port range is needed.
type STUNServer struct {
	conn *net.UDPConn
	done chan struct{}
	once sync.Once
}

func ListenSTUN(address string) (*STUNServer, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	s := &STUNServer{conn: conn, done: make(chan struct{})}
	go s.serve()
	return s, nil
}

func (s *STUNServer) Addr() *net.UDPAddr { return s.conn.LocalAddr().(*net.UDPAddr) }
func (s *STUNServer) Close() error {
	var err error
	s.once.Do(func() { err = s.conn.Close(); <-s.done })
	return err
}

func (s *STUNServer) serve() {
	defer close(s.done)
	type bucket struct {
		second int64
		count  int
	}
	rates := map[string]bucket{}
	var total bucket
	var buffer [2048]byte
	for {
		n, addr, err := s.conn.ReadFromUDP(buffer[:])
		if err != nil {
			return
		}
		if n == len(buffer) || !stun.IsMessage(buffer[:n]) {
			continue
		}
		now := time.Now().Unix()
		if total.second != now {
			total = bucket{second: now}
			for ip, value := range rates {
				if value.second < now-1 {
					delete(rates, ip)
				}
			}
		}
		ip := addr.IP.String()
		value := rates[ip]
		if value.second != now {
			value = bucket{second: now}
		}
		if total.count >= 5000 || value.count >= 100 || len(rates) >= 10000 && value.count == 0 {
			continue
		}
		total.count++
		value.count++
		rates[ip] = value
		request := stun.New()
		request.Raw = buffer[:n]
		if request.Decode() != nil || request.Type != stun.BindingRequest {
			continue
		}
		if request.Contains(stun.AttrFingerprint) && stun.Fingerprint.Check(request) != nil {
			continue
		}
		response, err := stun.Build(stun.NewTransactionIDSetter(request.TransactionID), stun.BindingSuccess,
			&stun.XORMappedAddress{IP: addr.IP, Port: addr.Port}, stun.Fingerprint)
		if err == nil {
			_, _ = s.conn.WriteToUDP(response.Raw, addr)
		}
	}
}
