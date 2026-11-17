//go:build !js || !wasm

package hubui

import "testing"

func testBrowserScreenAwake(t *testing.T) { t.Skip("requires a browser wake-lock implementation") }
