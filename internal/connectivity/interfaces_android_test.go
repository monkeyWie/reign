//go:build android

package connectivity

import (
	"testing"

	"github.com/pion/transport/v3/stdnet"
	"github.com/wlynxg/anet"
)

// Run under an ordinary Android app UID, where RTM_GETLINK is restricted.
func TestAndroidDirectCandidateInterfaces(t *testing.T) {
	n, err := stdnet.NewNet()
	if err != nil {
		t.Fatalf("Pion cannot enumerate Android interfaces: %v", err)
	}
	interfaces, err := n.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Fatalf("missing Pion interfaces: %v", err)
	}
	addresses, err := anet.InterfaceAddrs()
	if err != nil || len(addresses) == 0 {
		t.Fatalf("missing TCP candidate addresses: %v", err)
	}
	for _, address := range addresses {
		t.Log("candidate address:", address.String())
	}
}
