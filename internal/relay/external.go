package relay

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/nativepath"
)

const maxLeaseSeconds = 60

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var b64 = base64.RawURLEncoding.Strict()

func authorizationKey(key ed25519.PrivateKey) []byte {
	m := hmac.New(sha256.New, key.Seed())
	m.Write([]byte("reign/relay/credential/v1"))
	return m.Sum(nil)
}

// Credential exports the public TLS pin and a domain-separated ticket signing key.
func Credential(key ed25519.PrivateKey) string {
	return "rh1." + nativepath.Pin(key) + "." + b64.EncodeToString(authorizationKey(key))
}

type RelayNode struct {
	nativepath.Endpoint
	secret []byte
}
type NodeConfig struct {
	Addresses  connectivity.TransportAddresses `json:"addresses"`
	Credential string                          `json:"credential"`
}

func ResolveNodes(nodes []NodeConfig) ([]RelayNode, error) {
	if len(nodes) > 32 {
		return nil, errors.New("at most 32 external relays are supported")
	}
	seen := make(map[string]bool)
	out := make([]RelayNode, 0, len(nodes))
	for _, node := range nodes {
		parts := strings.Split(node.Credential, ".")
		if len(parts) != 3 || parts[0] != "rh1" {
			return nil, errors.New("invalid external relay credential")
		}
		pin := parts[1]
		pub, err := b64.DecodeString(pin)
		secret, keyErr := b64.DecodeString(parts[2])
		if err != nil || keyErr != nil || len(pin) != 43 || len(parts[2]) != 43 || len(pub) != 32 || len(secret) != 32 || seen[pin] {
			return nil, errors.New("invalid or duplicate external relay credential")
		}
		addrs, err := node.Addresses.Resolve()
		if err != nil {
			return nil, err
		}
		if len(addrs.UDP) > 1 || len(addrs.TCP) > 1 || len(addrs.UDP)+len(addrs.TCP) == 0 {
			return nil, errors.New("each external relay requires an address and supports one endpoint per transport")
		}
		endpoint := nativepath.Endpoint{Pin: pin}
		if len(addrs.UDP) > 0 {
			endpoint.UDPAddress = addrs.UDP[0]
		}
		if len(addrs.TCP) > 0 {
			endpoint.TCPAddress = addrs.TCP[0]
		}
		if endpoint.UDPAddress == endpoint.TCPAddress {
			endpoint.Address = endpoint.UDPAddress
			endpoint.UDPAddress, endpoint.TCPAddress = "", ""
		}
		seen[pin] = true
		out = append(out, RelayNode{endpoint, secret})
	}
	return out, nil
}

// The host leg belongs to its signal generation. The controller leg binds both
// that generation and the controller peer allocated by the same control.
func (node RelayNode) issue(host, generation, peer, role string, until time.Time) *NativeWelcome {
	payload := strings.Join([]string{"rh2", host, generation, peer, role, strconv.FormatInt(until.Unix(), 10)}, ".")
	mac := hmac.New(sha256.New, node.secret)
	mac.Write([]byte(payload))
	return &NativeWelcome{Endpoint: node.Endpoint, Ticket: payload + "." + b64.EncodeToString(mac.Sum(nil))}
}

func verifyTicket(secret []byte, join NativeJoin, now time.Time) (generation string, expiry time.Time, ok bool) {
	fields := strings.Split(join.Ticket, ".")
	if len(fields) != 7 || fields[0] != "rh2" || fields[1] != join.Host || fields[4] != join.Role || !idPattern.MatchString(join.Host) || !idPattern.MatchString(join.Peer) || !idPattern.MatchString(fields[2]) {
		return
	}
	if (join.Role == "client" && fields[3] != join.Peer) || (join.Role == "host" && fields[3] != fields[2]) {
		return
	}
	seconds, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil || seconds <= now.Unix() || seconds > now.Unix()+maxLeaseSeconds+5 {
		return
	}
	sig, err := b64.DecodeString(fields[6])
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strings.Join(fields[:6], ".")))
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return
	}
	return fields[2], time.Unix(seconds, 0), true
}

func (s *Server) welcome(hostID, authority, generation, peer, role string) *NativeWelcome {
	count := len(s.ExternalRelays)
	if s.nativePort != "" {
		count++
	}
	if count == 0 {
		return nil
	}
	n, _ := strconv.ParseUint(hostID[:8], 16, 32)
	idx := int(n % uint64(count))
	if idx < len(s.ExternalRelays) {
		return s.ExternalRelays[idx].issue(hostID, generation, peer, role, time.Now().Add(maxLeaseSeconds*time.Second))
	}
	address := s.nativePublic
	if address == "" {
		host := authority
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		address = net.JoinHostPort(host, s.nativePort)
	}
	return &NativeWelcome{Endpoint: nativepath.Endpoint{Address: address, Pin: s.nativePin}, Ticket: ID() + ID()}
}

func IsExternalTicket(value string) bool { return strings.HasPrefix(value, "rh2.") }
