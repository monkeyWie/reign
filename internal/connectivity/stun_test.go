package connectivity

import (
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
	"github.com/pion/webrtc/v4"
)

func TestSTUNBindingAndMalformedPackets(t *testing.T) {
	server, err := ListenSTUN("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.DialUDP("udp", nil, server.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = client.Write([]byte("not a STUN request"))
	indication := stun.MustBuild(stun.TransactionID, stun.NewType(stun.MethodBinding, stun.ClassIndication))
	_, _ = client.Write(indication.Raw)
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	if _, err := client.Write(request.Raw); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	n, err := client.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	response := stun.New()
	response.Raw = buffer[:n]
	if response.Decode() != nil || response.Type != stun.BindingSuccess || response.TransactionID != request.TransactionID {
		t.Fatal("incorrect binding response or response to a malformed packet")
	}
	var mapped stun.XORMappedAddress
	if err := mapped.GetFrom(response); err != nil {
		t.Fatal(err)
	}
	local := client.LocalAddr().(*net.UDPAddr)
	if mapped.Port != local.Port || !mapped.IP.Equal(local.IP) || stun.Fingerprint.Check(response) != nil {
		t.Fatal("incorrect mapped endpoint or fingerprint")
	}
}

func TestPolicyRetainsBothSTUNSourcesAndRejectsTURN(t *testing.T) {
	policy := Policy{PublicSTUN: []string{"stun:public.example:3478"}, SelfSTUN: []string{"stun:private.example:26330", "stun:public.example:3478"}}
	servers := policy.Servers()
	if len(servers) != 2 || servers[0].URLs[0] != policy.PublicSTUN[0] || servers[1].URLs[0] != policy.SelfSTUN[0] {
		t.Fatal("public and own STUN must both participate", servers)
	}
	if filtered := STUNOnly([]webrtc.ICEServer{{URLs: []string{"turn:old.example:3478", "turns:old.example:5349"}, Username: "old", Credential: "old"}}); len(filtered) != 0 {
		t.Fatal("legacy TURN was enabled")
	}
	if got := STUNURL("[2001:db8::1]:26330", "26330"); got != "stun:[2001:db8::1]:26330" {
		t.Fatal("IPv6 endpoint", got)
	}
}

func TestOwnSTUNGathersCandidateWhenPublicEndpointIsUnreachable(t *testing.T) {
	server, err := ListenSTUN("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	policy := Policy{PublicSTUN: []string{"stun:127.0.0.1:9"}, SelfSTUN: []string{"stun:" + server.Addr().String()}}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{ICEServers: policy.Servers()})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	candidate := make(chan struct{}, 1)
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil && c.Typ == webrtc.ICECandidateTypeSrflx {
			select {
			case candidate <- struct{}{}:
			default:
			}
		}
	})
	if _, err := pc.CreateDataChannel("fixture", nil); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-candidate:
	case <-time.After(3 * time.Second):
		t.Fatal("own STUN failed to provide a public-address candidate while the public endpoint was unreachable")
	}
}
