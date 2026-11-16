//go:build !js

package webapi

import (
	"context"
	"encoding/json"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
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
	info := device.ClientInfo{Name: "Microsoft Edge 157", Kind: "browser", Platform: "windows"}
	ctx = device.WithClientInfo(ctx, info)
	named, e := api.loadIdentity(ctx, server.URL, "outdated name")
	if e != nil {
		t.Fatal(e)
	}
	if named["name"] != "Microsoft Edge 157" || named["id"] != first["id"] || named["public"] != first["public"] || named["token"] != first["token"] {
		t.Fatal("browser rename changed identity")
	}
	again, e := api.loadIdentity(context.Background(), server.URL, "")
	var stored device.ClientInfo
	raw, _ := json.Marshal(again["info"])
	_ = json.Unmarshal(raw, &stored)
	if stored != info {
		t.Fatalf("profile not persisted: %#v", stored)
	}
	if e != nil || again["name"] != "Microsoft Edge 157" {
		t.Fatal("reconnect erased browser name", e)
	}
}
