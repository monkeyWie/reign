package clienthub

import (
	"context"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestControllerRenameKeepsPairingIdentity(t *testing.T) {
	svc, err := deviceservice.New(filepath.Join(t.TempDir(), "devices.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(svc)
	defer server.Close()
	hub, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	ctx := context.Background()
	first, err := hub.identity(ctx, server.URL, "RAS")
	if err != nil {
		t.Fatal(err)
	}
	info := device.ClientInfo{Name: "Pixel 9", Platform: "android", Kind: "native", Model: "Pixel 9"}
	ctx = device.WithClientInfo(ctx, info)
	named, err := hub.identity(ctx, server.URL, "outdated name")
	if err != nil {
		t.Fatal(err)
	}
	if named.Info != info || named.Name != "Pixel 9" || named.ID != first.ID || named.Public != first.Public || named.Token != first.Token {
		t.Fatal("rename replaced identity or was not persisted")
	}
	again, err := hub.identity(context.Background(), server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "Pixel 9" || again.Info != info {
		t.Fatal("background reconnect overwrote the resolved name")
	}
}
