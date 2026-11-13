//go:build !js

package webapi

import (
	"context"
	"github.com/monkeyWie/reign/internal/deviceservice"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestBrowserRenameKeepsStoredIdentity(t *testing.T) {
	service, e := deviceservice.New(filepath.Join(t.TempDir(), "devices.json"), nil)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	api := New(&memoryStore{v: map[string][]byte{}})
	defer api.Close()
	ctx := context.Background()
	first, e := api.loadIdentity(ctx, server.URL, "RAS")
	if e != nil {
		t.Fatal(e)
	}
	named, e := api.loadIdentity(ctx, server.URL, "Chrome · Mac")
	if e != nil {
		t.Fatal(e)
	}
	if named["name"] != "Chrome · Mac" || named["id"] != first["id"] || named["public"] != first["public"] || named["token"] != first["token"] {
		t.Fatal("browser rename changed identity")
	}
	again, e := api.loadIdentity(ctx, server.URL, "")
	if e != nil || again["name"] != "Chrome · Mac" {
		t.Fatal("reconnect erased browser name", e)
	}
}
