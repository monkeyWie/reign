//go:build !js

package webapi

import "testing"

func browserStorage(t *testing.T) { t.Skip("IndexedDB requires a browser") }
