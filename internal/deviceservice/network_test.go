package deviceservice

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

func TestNetworkDiscoveryAdvertisesReachableSTUNHost(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.STUNPort = "26331"
	s.TCPSTUNPort = "26331"
	request := httptest.NewRequest("POST", "https://hub.example:26331/p2p/config", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	s.ServeHTTP(response, request)
	var policy connectivity.Policy
	if err := json.Unmarshal(response.Body.Bytes(), &policy); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(policy.PublicSTUN) == 0 || len(policy.SelfSTUN) != 1 || policy.SelfSTUN[0] != "stun:hub.example:26331" {
		t.Fatal("incorrect public/self-hosted discovery", policy)
	}
	if len(policy.TCPSTUN) != 1 || policy.TCPSTUN[0] != "hub.example:26331" {
		t.Fatal("missing separate TCP discovery", policy)
	}
	s.Network.SelfSTUN = []string{"stun:udp.example:26331"}
	s.Network.TCPSTUN = []string{"tcp.example:26332"}
	request = httptest.NewRequest("POST", "https://hub.example/p2p/config", strings.NewReader(`{}`))
	response = httptest.NewRecorder()
	s.ServeHTTP(response, request)
	_ = json.Unmarshal(response.Body.Bytes(), &policy)
	if policy.SelfSTUN[0] != "stun:udp.example:26331" {
		t.Fatal("reverse-proxy STUN override lost")
	}
	if len(policy.TCPSTUN) != 1 || policy.TCPSTUN[0] != "tcp.example:26332" {
		t.Fatal("TCP override lost behind proxy", policy)
	}
}
