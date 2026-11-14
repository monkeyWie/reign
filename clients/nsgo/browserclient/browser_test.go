package browserclient

import "testing"

func TestBrowserP2PAndRelayWithExistingHost(t *testing.T) { browserInterop(t) }
func TestBrowserDirectWithoutRelay(t *testing.T)          { browserDirectInterop(t) }
