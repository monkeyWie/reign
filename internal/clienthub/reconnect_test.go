package clienthub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/internal/remote"
)

type recoveryProvider struct{ provider.Provider }

func (recoveryProvider) Status(context.Context) provider.Status {
	return provider.Status{ID: "fixture", State: "ready"}
}

func TestReconnectReusesConnectionThenCachedPairingAndFallsBack(t *testing.T) {
	token := strings.Repeat("r", 32)
	host := relay.ID()
	key := strings.Repeat("ab", 32)
	var resolves atomic.Int32
	var rejectNext atomic.Bool
	transport := relay.New(token)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p2p/devices/resolve":
			resolves.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"online": true, "device": map[string]any{"id": host, "public": "host-public", "connectionId": "654321", "name": "test host"}})
		case "/p2p/signal":
			if r.URL.Query().Get("role") == "client" && rejectNext.Swap(false) {
				http.Error(w, "stale route", http.StatusServiceUnavailable)
				return
			}
			transport.ServeHTTP(w, r)
		default:
			transport.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pair := remote.Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: host, Key: key, RelayToken: token}
	status := remote.NewHostStatus(pair.Relay)
	go remote.RunHostObserved(ctx, pair, remote.Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": recoveryProvider{}}}, status)
	until := time.Now().Add(5 * time.Second)
	for status.Snapshot().State != "connected" {
		if time.Now().After(until) {
			t.Fatal("host not ready")
		}
		time.Sleep(time.Millisecond)
	}
	h, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	id := server.URL + "|" + host
	saved := Saved{ID: id, Service: server.URL, ConnectionID: "654321", Name: "test host", Host: host, Public: "host-public", Key: key, HasConnected: true, RelayOnly: true}
	if err = h.DB.SaveEnvironment(id, saved); err != nil {
		t.Fatal(err)
	}
	if err = h.DB.SaveIdentity(server.URL, "controller", device.Identity{Token: token}); err != nil {
		t.Fatal(err)
	}
	setup, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	if _, err = h.Reconnect(setup, id); err != nil {
		t.Fatal(err)
	}
	var stored Saved
	if err := h.DB.LoadEnvironment(id, &stored); err != nil || stored.Platform != runtime.GOOS {
		t.Fatalf("existing pairing did not learn the verified host platform: %q, %v", stored.Platform, err)
	}
	rows, err := h.List()
	if err != nil || len(rows) != 1 || rows[0]["platform"] != stored.Platform {
		t.Fatal("verified host platform missing from device list")
	}
	first := h.connections[id]
	if resolves.Load() != 0 {
		t.Fatal("cached reconnect resolved device again")
	}
	if _, err = h.Reconnect(setup, id); err != nil {
		t.Fatal(err)
	}
	if h.connections[id] != first || resolves.Load() != 0 {
		t.Fatal("healthy connection was replaced")
	}
	first.Close()
	if _, err = h.Reconnect(setup, id); err != nil {
		t.Fatal(err)
	}
	if h.connections[id] == first || resolves.Load() != 0 {
		t.Fatal("lost socket did not recover with cached pairing")
	}
	h.connections[id].Close()
	rejectNext.Store(true)
	if _, err = h.Reconnect(setup, id); err != nil {
		t.Fatal(err)
	}
	if resolves.Load() != 1 {
		t.Fatalf("full discovery fallback count: %d", resolves.Load())
	}
	saved.ReconnectDisabled = true
	if err = h.DB.SaveEnvironment(id, saved); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Reconnect(setup, id); err == nil {
		t.Fatal("manual disconnect was ignored")
	}
}
