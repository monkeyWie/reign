package deviceservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/device"
)

func TestBatchPresenceUsesSignalAndRespectsPairing(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	register := func(host bool) device.Identity {
		t.Helper()
		i, _ := device.NewIdentity("host")
		i, err = device.Register(ctx, server.URL, i, host)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}
	host, client := register(true), register(false)
	query := func() []device.DeviceStatus {
		return device.Presence(ctx, []device.PresenceTarget{{ID: "saved", Service: server.URL, Host: host.ID, Token: client.Token}})
	}
	if state := query()[0]; state.Online != nil || state.Name != "" {
		t.Fatal("unpaired host disclosed presence", state)
	}
	if err = s.DB.Create(&DeviceGrant{Host: host.ID, Client: client.ID}).Error; err != nil {
		t.Fatal(err)
	}
	// A recent registration/heartbeat alone must not report a host as online.
	if state := query()[0]; state.Online == nil || *state.Online {
		t.Fatal("host without signal is online", state)
	}
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/p2p/signal?id="+host.ID+"&role=host", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + host.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if _, _, err = ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	if state := query()[0]; state.Online == nil || !*state.Online || state.ID != "saved" {
		t.Fatal("connected host missing", state)
	}
	ws.CloseNow()
	for s.relay.Online(host.ID) {
		if ctx.Err() != nil {
			t.Fatal("closed signal still online")
		}
		time.Sleep(time.Millisecond)
	}
	if state := query()[0]; state.Online == nil || *state.Online {
		t.Fatal("closed host is online", state)
	}
	if err = s.DB.Delete(&DeviceGrant{}, "host = ? AND client = ?", host.ID, client.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state := query()[0]; state.Online != nil {
		t.Fatal("revoked host disclosed presence", state)
	}
}
