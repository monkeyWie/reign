//go:build !js

package webapi

import "testing"

func deployedBrowserStability(t *testing.T) { t.Skip("requires a real browser") }
