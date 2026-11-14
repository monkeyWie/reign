package deviceservice

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/device"
)

func TestIdentityAndDeviceRouteIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	service, e := New(path, []string{"https://web.example"})
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	codes := map[string]bool{}
	register := func(name string, host bool) device.Identity {
		i, e := device.NewIdentity(name)
		if e != nil {
			t.Fatal(e)
		}
		i, e = device.Register(ctx, server.URL, i, host)
		if e != nil {
			t.Fatal(e)
		}
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(i.ConnectionID) || codes[i.ConnectionID] {
			t.Fatal("device ID must be six digits and unique")
		}
		codes[i.ConnectionID] = true
		return i
	}
	h1 := register("host1", true)
	h2 := register("host2", true)
	client := register("client", false)
	disk, _ := os.ReadFile(path)
	if strings.Contains(string(disk), client.Token) || strings.Contains(string(disk), h1.Token) {
		t.Fatal("raw service token persisted")
	}
	restarted, e := New(path, nil)
	if e != nil || restarted.ServiceID != service.ServiceID {
		t.Fatal("service identity not durable", e)
	}
	dial := func(i device.Identity, host, role string) (*websocket.Conn, error) {
		ws, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/p2p/signal?id="+host+"&role="+role, &websocket.DialOptions{HTTPHeader: map[string][]string{"Authorization": {"Bearer " + i.Token}}})
		return ws, e
	}
	if ws, e := dial(client, h1.ID, "client"); e == nil {
		ws.CloseNow()
		t.Fatal("unpaired client admitted")
	}
	if ws, e := dial(h1, h2.ID, "host"); e == nil {
		ws.CloseNow()
		t.Fatal("host impersonation allowed")
	}
	if ws, e := dial(client, client.ID, "host"); e == nil {
		ws.CloseNow()
		t.Fatal("client registered host role")
	}
	if err := service.DB.Create(&DeviceGrant{Host: h1.ID, Client: client.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if ws, e := dial(client, h2.ID, "client"); e == nil {
		ws.CloseNow()
		t.Fatal("grant crossed host boundary")
	}
	hostWS, e := dial(h1, h1.ID, "host")
	if e != nil {
		t.Fatal(e)
	}
	defer hostWS.CloseNow()
	if _, _, e = hostWS.Read(ctx); e != nil {
		t.Fatal(e)
	}
	clientWS, e := dial(client, h1.ID, "client")
	if e != nil {
		t.Fatal(e)
	}
	defer clientWS.CloseNow()
	if _, _, e = clientWS.Read(ctx); e != nil {
		t.Fatal(e)
	}
	// The relay stamps the authenticated device, ignoring a forged caller identity.
	if e = clientWS.Write(ctx, websocket.MessageText, []byte(`{"kind":"hello","device":"forged","peer":"forged"}`)); e != nil {
		t.Fatal(e)
	}
	_, raw, e := hostWS.Read(ctx)
	if e != nil || !strings.Contains(string(raw), client.ID) || strings.Contains(string(raw), "forged") {
		t.Fatalf("untrusted identity forwarded: %s %v", raw, e)
	}
	if e = device.Post(ctx, server.URL, h2.Token, "/p2p/pairings/revoke", map[string]string{"client": client.ID}, nil); e != nil {
		t.Fatal(e)
	}
	var grant DeviceGrant
	still := service.DB.First(&grant, "host = ? AND client = ?", h1.ID, client.ID).Error == nil
	if !still {
		t.Fatal("another host revoked grant")
	}
	if e = device.Post(ctx, server.URL, h1.Token, "/p2p/pairings/revoke", map[string]string{"client": client.ID}, nil); e != nil {
		t.Fatal(e)
	}
	if _, _, e = clientWS.Read(ctx); e == nil {
		t.Fatal("revoked WebSocket stayed open")
	}
}
