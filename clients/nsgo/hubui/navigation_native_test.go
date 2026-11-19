//go:build !js

package hubui

import (
	"testing"

	"github.com/nsgo-dev/nsgo/nsgotest"
)

func pressWorkspaceBack(t *testing.T, app *nsgotest.App) {
	t.Helper()
	app.Back()
}

func testBrowserBackStaysInCompactWorkspace(t *testing.T) { t.Skip("requires real browser history") }
func testBrowserBackStaysInWideWorkspace(t *testing.T)    { t.Skip("requires real browser history") }
