package clienthub

import (
	"context"
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
	named, err := hub.identity(ctx, server.URL, "Pixel 9")
	if err != nil {
		t.Fatal(err)
	}
	if named.Name != "Pixel 9" || named.ID != first.ID || named.Public != first.Public || named.Token != first.Token {
		t.Fatal("rename replaced identity or was not persisted")
	}
	again, err := hub.identity(ctx, server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if again.Name != "Pixel 9" {
		t.Fatal("background reconnect overwrote the resolved name")
	}
}
