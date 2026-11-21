// Package connectivity contains the public/self-hosted STUN discovery policy.
// The signal welcome separately advertises optional native relay transport.
package connectivity

import (
	"net"
	"strings"

	"github.com/pion/webrtc/v4"
)

const DefaultSTUNPort = "26331"

type Policy struct {
	Deployment     string   `json:"deployment"`
	ServiceID      string   `json:"serviceId"`
	GrantPublicKey string   `json:"grantPublicKey"`
	PublicSTUN     []string `json:"publicStun"`
	SelfSTUN       []string `json:"selfStun"`
	TCPSTUN        []string `json:"tcpStun,omitempty"` // host:port, TCP transport (not ICE URLs)
}

func DefaultPolicy() Policy {
	return Policy{PublicSTUN: []string{"stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"}, SelfSTUN: []string{}}
}

func STUNOnly(servers []webrtc.ICEServer) []webrtc.ICEServer {
	result := []webrtc.ICEServer{}
	seen := map[string]bool{}
	for _, server := range servers {
		for _, raw := range server.URLs {
			if strings.HasPrefix(raw, "stun:") && !seen[raw] {
				result = append(result, webrtc.ICEServer{URLs: []string{raw}})
				seen[raw] = true
			}
		}
	}
	return result
}

func (p Policy) Servers() []webrtc.ICEServer {
	return STUNOnly([]webrtc.ICEServer{{URLs: p.PublicSTUN}, {URLs: p.SelfSTUN}})
}

func STUNURL(host, port string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" || port == "" {
		return ""
	}
	return "stun:" + net.JoinHostPort(host, port)
}
