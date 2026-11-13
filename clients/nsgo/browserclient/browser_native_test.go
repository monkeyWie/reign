//go:build !js

package browserclient

import "testing"

func browserInterop(t *testing.T) { t.Skip("browser-only integration") }
