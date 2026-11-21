package connectivity

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var endpointHost = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)

// TransportAddresses are public endpoints. A bare endpoint serves both transports.
type TransportAddresses []string

type Endpoints struct{ UDP, TCP []string }

func (entries TransportAddresses) Resolve() (Endpoints, error) {
	var out Endpoints
	if len(entries) > 32 {
		return out, fmt.Errorf("at most 32 addresses are supported")
	}
	for _, value := range entries {
		address := value
		protocol := ""
		for _, prefix := range []string{"udp:", "tcp:"} {
			if strings.HasPrefix(value, prefix) {
				address, protocol = strings.TrimPrefix(value, prefix), prefix
				break
			}
		}
		host, port, err := net.SplitHostPort(address)
		n, errPort := strconv.ParseUint(port, 10, 16)
		validHost := endpointHost.MatchString(host)
		if strings.HasPrefix(address, "[") {
			ip, errIP := netip.ParseAddr(host)
			validHost = errIP == nil && ip.Is6() && ip.Zone() == ""
		}
		if err != nil || errPort != nil || n == 0 || !validHost {
			return Endpoints{}, fmt.Errorf("expected host:port, udp:host:port or tcp:host:port")
		}
		if protocol != "tcp:" && !slices.Contains(out.UDP, address) {
			out.UDP = append(out.UDP, address)
		}
		if protocol != "udp:" && !slices.Contains(out.TCP, address) {
			out.TCP = append(out.TCP, address)
		}
	}
	return out, nil
}

func (e Endpoints) AppendSTUN(p *Policy) {
	p.SelfSTUN = slices.Clip(p.SelfSTUN)
	for _, addr := range e.UDP {
		url := "stun:" + addr
		if !slices.Contains(p.SelfSTUN, url) {
			p.SelfSTUN = append(p.SelfSTUN, url)
		}
	}
	p.TCPSTUN = slices.Clip(p.TCPSTUN)
	for _, addr := range e.TCP {
		if !slices.Contains(p.TCPSTUN, addr) {
			p.TCPSTUN = append(p.TCPSTUN, addr)
		}
	}
}
