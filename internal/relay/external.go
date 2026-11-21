// Static external native relays authenticate against one operator-configured
// control origin. Devices never choose the callback URL.
package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/nativepath"
)

const NativeLeasePath = "/p2p/native-relay/lease"
const maxLeaseSeconds = 60
const leaseDomain = "reign/native-relay/lease/v1\n"

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var ticketPattern = regexp.MustCompile(`^ext1\.[a-f0-9]{64}$`)

type NodeConfig struct {
	Addresses connectivity.TransportAddresses `json:"addresses"`
	Pin       string                          `json:"pin"`
}

func ResolveNodes(nodes []NodeConfig) ([]nativepath.Endpoint, error) {
	if len(nodes) > 32 {
		return nil, errors.New("at most 32 external relays are supported")
	}
	seen := make(map[string]bool)
	out := make([]nativepath.Endpoint, 0, len(nodes))
	for _, node := range nodes {
		key, err := base64.RawURLEncoding.Strict().DecodeString(node.Pin)
		if err != nil || len(node.Pin) != 43 || len(key) != ed25519.PublicKeySize || seen[node.Pin] {
			return nil, errors.New("external relays require a unique base64url Ed25519 pin")
		}
		addrs, err := node.Addresses.Resolve()
		if err != nil {
			return nil, err
		}
		if len(addrs.UDP) > 1 || len(addrs.TCP) > 1 || len(addrs.UDP)+len(addrs.TCP) == 0 {
			return nil, errors.New("each external relay requires an address and supports one endpoint per transport")
		}
		endpoint := nativepath.Endpoint{Pin: node.Pin}
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
		seen[node.Pin] = true
		out = append(out, endpoint)
	}
	return out, nil
}

func ValidControlOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil || n == 0 {
			return false
		}
	}
	ip, _ := netip.ParseAddr(u.Hostname())
	return u.Scheme == "https" || u.Scheme == "http" && (ip.IsLoopback() || u.Hostname() == "localhost")
}

// Reserve one slot for the colocated listener if present. The host ID is a
// uniformly random directory identifier, common to all sockets in its room.
func (s *Server) welcome(hostID, authority string) *NativeWelcome {
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
		return &NativeWelcome{Endpoint: s.ExternalRelays[idx], Ticket: "ext1." + ID() + ID()}
	}
	address := s.nativePublic
	if address == "" {
		host := authority
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		address = net.JoinHostPort(strings.Trim(host, "[]"), s.nativePort)
	}
	return &NativeWelcome{Endpoint: nativepath.Endpoint{Address: address, Pin: s.nativePin}, Ticket: ID() + ID()}
}

type leaseCheck struct {
	Host         string `json:"host"`
	Peer         string `json:"peer"`
	HostTicket   string `json:"hostTicket"`
	ClientTicket string `json:"clientTicket"`
}

// The TLS pin doubles as a public verification key for domain-separated checks.
func (s *Server) serveLease(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	pin := r.Header.Get("X-Reign-Relay-Pin")
	var allowed bool
	for _, node := range s.ExternalRelays {
		if node.Pin == pin {
			allowed = true
			break
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	pub, _ := base64.RawURLEncoding.DecodeString(pin)
	sig, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Reign-Relay-Signature"))
	var check leaseCheck
	if !allowed || err != nil || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, append([]byte(leaseDomain), raw...), sig) || json.Unmarshal(raw, &check) != nil {
		w.WriteHeader(403)
		return
	}
	s.mu.Lock()
	room := s.rooms[check.Host]
	valid := room != nil && room.clients[check.Peer] != nil && check.HostTicket != "" && check.ClientTicket != ""
	if valid {
		host, client := room.host, room.clients[check.Peer]
		valid = host.nativePin == pin && client.nativePin == pin &&
			subtle.ConstantTimeCompare([]byte(check.HostTicket), []byte(host.nativeTicket)) == 1 &&
			subtle.ConstantTimeCompare([]byte(check.ClientTicket), []byte(client.nativeTicket)) == 1
	}
	s.mu.Unlock()
	if !valid {
		w.WriteHeader(403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"validFor": maxLeaseSeconds})
}

type externalAuthority struct {
	origin string
	key    ed25519.PrivateKey
	pin    string
	client http.Client
}

func (a *externalAuthority) lease(ctx context.Context, check leaseCheck) (time.Time, error) {
	start := time.Now()
	raw, err := json.Marshal(check)
	if err != nil {
		return time.Time{}, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, a.origin+NativeLeasePath, bytes.NewReader(raw))
	if err != nil {
		return time.Time{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Reign-Relay-Pin", a.pin)
	r.Header.Set("X-Reign-Relay-Signature", base64.RawURLEncoding.EncodeToString(ed25519.Sign(a.key, append([]byte(leaseDomain), raw...))))
	resp, err := a.client.Do(r)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	var lease struct {
		ValidFor int `json:"validFor"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 256)).Decode(&lease) != nil || lease.ValidFor <= 0 || lease.ValidFor > maxLeaseSeconds {
		return time.Time{}, errors.New("external relay lease denied")
	}
	return start.Add(time.Duration(lease.ValidFor) * time.Second), nil
}

// Only one leg refreshes the pair. A slow or unavailable authority cannot
// extend the last lease beyond its deadline.
func (a *externalAuthority) maintain(ctx context.Context, check leaseCheck, until time.Time, closeLink func()) {
	for {
		delay := time.Until(until) - 5*time.Second
		if delay <= 0 {
			delay = max(0, time.Until(until)/2)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		refresh, cancel := context.WithDeadline(ctx, until)
		next, err := a.lease(refresh, check)
		cancel()
		if err != nil || !next.After(time.Now()) {
			closeLink()
			return
		}
		until = next
	}
}
