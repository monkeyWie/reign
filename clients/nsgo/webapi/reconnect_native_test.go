//go:build !js

package webapi

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
		case "/p2p/devices/presence":
			json.NewEncoder(w).Encode(map[string]any{})
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
	ctx, cancel := context.WithCancel(device.WithClientInfo(context.Background(), device.ClientInfo{Name: "Microsoft Edge 157", Platform: "windows", Kind: "browser"}))
	defer cancel()
	pair := remote.Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: host, Key: key, RelayToken: token}
	status := remote.NewHostStatus(pair.Relay)
	profiles := make(chan device.ClientInfo, 16)
	opts := remote.Options{RelayOnly: true, ResolveKey: func(string) (string, bool) { return key, true }, OnClientInfo: func(_ string, info device.ClientInfo) error {
		select {
		case profiles <- info:
		default:
		}
		return nil
	}}
	go remote.RunHostObserved(ctx, pair, opts, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": recoveryProvider{}}}, status)
	until := time.Now().Add(5 * time.Second)
	for status.Snapshot().State != "connected" {
		if time.Now().After(until) {
			t.Fatal("host not ready")
		}
		time.Sleep(time.Millisecond)
	}
	storage := &memoryStore{v: map[string][]byte{}}
	h := New(storage)
	defer h.Close()
	var err error
	id := server.URL + "|" + host
	saved := Object{"id": id, "service": server.URL, "connectionId": "654321", "name": "test host", "host": host, "public": "host-public", "key": key, "relayOnly": true}
	if err = storage.SaveEnvironment(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	if err = storage.Put("identity:"+server.URL, Object{"token": token}); err != nil {
		t.Fatal(err)
	}
	reconnect := func(ctx context.Context) (any, error) {
		return h.Call(ctx, "client.reconnect", Object{"id": id}, "", nil)
	}
	setup, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	if _, err = reconnect(setup); err != nil {
		t.Fatal(err)
	}
	select {
	case profile := <-profiles:
		if profile.Name != "Microsoft Edge 157" || profile.Platform != "windows" || profile.Kind != "browser" {
			t.Fatalf("incorrect browser profile: %#v", profile)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("browser did not publish authenticated profile")
	}
	devices, err := h.saved()
	if err != nil || len(devices) != 1 || str(devices[0], "platform") != runtime.GOOS {
		t.Fatal("browser pairing did not retain the verified host platform")
	}
	first := h.client(id)
	if resolves.Load() != 0 {
		t.Fatal("cached reconnect resolved device again")
	}
	if _, err = reconnect(device.WithClientInfo(setup, device.ClientInfo{Name: "Microsoft Edge 158", Platform: "windows", Kind: "browser"})); err != nil {
		t.Fatal(err)
	}
	select {
	case profile := <-profiles:
		if profile.Name != "Microsoft Edge 158" {
			t.Fatalf("resume did not update browser profile: %#v", profile)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume did not publish browser profile")
	}
	if h.client(id) != first || resolves.Load() != 0 {
		t.Fatal("healthy connection was replaced")
	}
	first.Close()
	if _, err = reconnect(setup); err != nil {
		t.Fatal(err)
	}
	if h.client(id) == first || resolves.Load() != 0 {
		t.Fatal("lost socket did not recover with cached pairing")
	}
	h.client(id).Close()
	rejectNext.Store(true)
	if _, err = reconnect(setup); err != nil {
		t.Fatal(err)
	}
	if resolves.Load() != 1 {
		t.Fatalf("full discovery fallback count: %d", resolves.Load())
	}
	saved["reconnectDisabled"] = true
	if err = storage.SaveEnvironment(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	if _, err = reconnect(setup); err == nil {
		t.Fatal("manual disconnect was ignored")
	}
}
