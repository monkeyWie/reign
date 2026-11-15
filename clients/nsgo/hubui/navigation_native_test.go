//go:build !js

package hubui

import "testing"

func testBrowserBackStaysInCompactWorkspace(t *testing.T) { t.Skip("requires real browser history") }
func testBrowserBackStaysInWideWorkspace(t *testing.T)    { t.Skip("requires real browser history") }
