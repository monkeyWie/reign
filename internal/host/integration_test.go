package host_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reign/internal/clienthub"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/deviceservice"
	"github.com/monkeyWie/reign/internal/host"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/remote"
	"github.com/monkeyWie/reign/internal/store"
	"github.com/monkeyWie/reign/internal/syncstate"
	"github.com/monkeyWie/reign/internal/testagent"
)

func eventually(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition timeout")
}
func invoke(t *testing.T, m *host.Manager, method string, params any) any {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, e := m.Local(context.Background(), control.Request{Method: method, Params: raw})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestDevicePairingControlAndRevocation(t *testing.T) {
	service, e := deviceservice.New(filepath.Join(t.TempDir(), "registry.json"), nil)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cfg, e := config.Load(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	cfg.ServiceURL = server.URL
	fake := testagent.New()
	d := &control.Dispatcher{Providers: map[string]provider.Provider{"demo": fake}}
	db, e := store.Open(filepath.Join(cfg.DataDir, "host.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	d.Execute = db.Execute
	idx := &syncstate.Index{DB: db, Epoch: device.Random()}
	d.Sync = func(ctx context.Context, p provider.Provider, r control.Request) (any, error) {
		return idx.Sync(ctx, p, r.Provider, r.Epoch, r.After)
	}
	manager := host.New(cfg, d, func(config.Config) remote.Options { return remote.Options{RelayOnly: true} })
	d.Local = manager.Local
	done := make(chan struct{})
	go func() { defer close(done); manager.Run(ctx) }()
	defer func() { cancel(); <-done }()
	var info map[string]any
	eventually(t, func() bool {
		info = invoke(t, manager, "host.info", nil).(map[string]any)
		return info["connectionId"] != "" && manager.Network().State == "connected"
	})
	code := info["connectionId"].(string)
	hostID := info["deviceId"].(string)
	// A valid service identity cannot read any host data before approval.
	intruder, _ := device.NewIdentity("not paired")
	intruder, e = device.Register(ctx, server.URL, intruder, false)
	if e != nil {
		t.Fatal(e)
	}
	ws, resp, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/relay?role=client&id="+hostID, &websocket.DialOptions{HTTPHeader: map[string][]string{"Authorization": {"Bearer " + intruder.Token}}})
	if e == nil {
		ws.CloseNow()
		t.Fatal("unpaired controller was allowed")
	}
	if resp == nil || resp.StatusCode != 401 {
		t.Fatalf("expected unauthorized, got %v %v", resp, e)
	}
	hub, e := clienthub.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer hub.DB.Close()
	result := make(chan error, 1)
	var saved clienthub.Saved
	go func() {
		var err error
		saved, err = hub.Connect(ctx, server.URL, code, info["public"].(string), "", "test controller", true, true)
		result <- err
	}()
	var pending []map[string]any
	eventually(t, func() bool {
		pending = invoke(t, manager, "host.info", nil).(map[string]any)["pending"].([]map[string]any)
		return len(pending) == 1
	})
	invoke(t, manager, "host.respond", map[string]any{"id": pending[0]["id"], "allow": true})
	if e = <-result; e != nil {
		t.Fatal(e)
	}
	defer hub.Disconnect(saved.ID)
	directory, e := hub.Sync(ctx, saved.ID, "demo")
	if e != nil || len(directory.([]map[string]any)) != 1 {
		t.Fatalf("initial sync: %v %v", directory, e)
	}
	command := control.Request{ID: "durable-command", Method: "send", Provider: "demo", Session: "existing", Text: "integration hello"}
	if _, e = hub.Call(ctx, saved.ID, command); e != nil {
		t.Fatal(e)
	}
	// Explicit retries in the same connection return the recorded result too.
	if _, e = hub.Call(ctx, saved.ID, command); e != nil {
		t.Fatal(e)
	}
	// Same command after reconnect must not cause a second side effect.
	hub.Disconnect(saved.ID)
	if _, e = hub.Connect(ctx, server.URL, code, "", "", "test controller", true, true); e != nil {
		t.Fatal(e)
	}
	if _, e = hub.Call(ctx, saved.ID, command); e != nil {
		t.Fatal(e)
	}
	if fake.Sends != 1 {
		t.Fatalf("command replayed %d times", fake.Sends)
	}
	if _, e = hub.Call(ctx, saved.ID, control.Request{Method: "host.info"}); e == nil {
		t.Fatal("remote lifecycle endpoint exposed")
	}
	invoke(t, manager, "host.enable", map[string]any{"provider": "demo"})
	if _, e = hub.Call(ctx, saved.ID, control.Request{Method: "create", Provider: "demo"}); e != nil {
		t.Fatal(e)
	}
	directory, e = hub.Sync(ctx, saved.ID, "demo")
	if e != nil || len(directory.([]map[string]any)) != 2 {
		t.Fatalf("incremental sync: %v %v", directory, e)
	}
	hub.Disconnect(saved.ID)
	directory, e = hub.Sync(ctx, saved.ID, "demo")
	if e != nil || len(directory.([]map[string]any)) != 2 {
		t.Fatalf("offline cache: %v %v", directory, e)
	}
	if _, e = hub.Call(ctx, saved.ID, command); e == nil {
		t.Fatal("offline write should fail")
	}
	// Keep the first controller connected while a second device pairs and is revoked.
	if _, e = hub.Connect(ctx, server.URL, code, "", "", "test controller", true, true); e != nil {
		t.Fatal(e)
	}
	// A second independent controller can pair by password without host confirmation.
	invoke(t, manager, "host.password", map[string]any{"password": "demo-password-123"})
	second, e := clienthub.New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer second.DB.Close()
	if _, e = second.Connect(ctx, server.URL, code, "", "wrong-password", "second", true, true); e == nil {
		t.Fatal("wrong password allowed")
	}
	s2, e := second.Connect(ctx, server.URL, code, "", "demo-password-123", "second", true, true)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Disconnect(s2.ID)
	grants := invoke(t, manager, "host.info", nil).(map[string]any)["grants"].([]map[string]any)
	var clientID string
	for _, g := range grants {
		if g["name"] == "second" {
			clientID = g["client"].(string)
		}
	}
	if clientID == "" {
		t.Fatal("second grant missing")
	}
	invoke(t, manager, "host.revoke", map[string]any{"client": clientID})
	short, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if _, e = second.Call(short, s2.ID, control.Request{Method: "sessions", Provider: "demo"}); e == nil {
		t.Fatal("revoked controller retained access")
	}
	if _, e = hub.Call(ctx, saved.ID, control.Request{Method: "providers"}); e != nil {
		t.Fatalf("revoking second controller disconnected first: %v", e)
	}
	if e = hub.Forget(saved.ID); e != nil {
		t.Fatal(e)
	}
	keys, _ := hub.DB.Keys("directory:")
	if len(keys) != 0 {
		t.Fatal("forgotten device cache retained")
	}
}

// Provider discovery/attachment must work even before any server is configured.
// It is independent of both pairing and the removed per-Agent access switch.
type autoAttachProvider struct {
	*testagent.Provider
	attached chan struct{}
	starts   atomic.Int32
}

func (p *autoAttachProvider) AutoConnect(context.Context) error {
	select {
	case p.attached <- struct{}{}:
	default:
	}
	return nil
}
func (p *autoAttachProvider) Start(context.Context) error { p.starts.Add(1); return nil }
func TestAutomaticAttachmentWithoutServiceOrAgentSwitch(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &autoAttachProvider{Provider: testagent.New(), attached: make(chan struct{}, 1)}
	d := &control.Dispatcher{Providers: map[string]provider.Provider{"demo": p}}
	m := host.New(cfg, d, func(config.Config) remote.Options { return remote.Options{} })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-p.attached:
	case <-time.After(time.Second):
		t.Fatal("no automatic attachment without configured service")
	}
	if p.starts.Load() != 0 {
		t.Fatal("automatic attachment invoked explicit launch")
	}
}
