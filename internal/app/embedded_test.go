package app

import (
	"context"
	"encoding/json"
	"github.com/monkeyWie/reignhub/internal/clienthub"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/testagent"
)

func TestEmbeddedCallsNeedNoLocalHTTPAndPreserveIdentity(t *testing.T) {
	service, err := deviceservice.New(filepath.Join(t.TempDir(), "devices.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(service)
	defer server.Close()
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// This port belongs to a different process. An embedded host must not bind
	// or probe it, or silently attach to a pre-existing helper there.
	c.Listen = occupied.Addr().String()
	c.ServiceURL = server.URL
	providers := map[string]provider.Provider{"demo": testagent.New()}
	r, err := startEmbedded(context.Background(), c, providers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	info := waitEmbeddedOnline(t, r)
	code := info["connectionId"].(string)
	if err := r.Call(context.Background(), control.Request{Method: "host.password", Params: []byte(`{"password":"test-password"}`)}, nil); err != nil {
		t.Fatal(err)
	}
	var list []provider.Status
	if err := r.Call(context.Background(), control.Request{Method: "providers"}, &list); err != nil || len(list) != 1 || list[0].ID != "demo" {
		t.Fatalf("direct provider call: %v %v", list, err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			var out map[string]any
			if err := r.Call(context.Background(), control.Request{Method: "host.info"}, &out); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Call(context.Background(), control.Request{Method: "providers"}, &list); err == nil {
		t.Fatal("closed runtime accepted a call")
	}
	reopened, err := startEmbedded(context.Background(), c, providers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	info = waitEmbeddedOnline(t, reopened)
	if info["connectionId"] != code || info["passwordEnabled"] != true {
		t.Fatal("reopening changed saved device identity or password")
	}
}

func waitEmbeddedOnline(t *testing.T, r *Embedded) map[string]any {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		var info map[string]any
		if err := r.Call(context.Background(), control.Request{Method: "host.info"}, &info); err != nil {
			t.Fatal(err)
		}
		network, _ := info["network"].(map[string]any)
		if info["connectionId"] != "" && network["state"] == "connected" {
			return info
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("embedded registration timed out")
	return nil
}

func TestRejectedSignalingDoesNotAdvertiseHostOnline(t *testing.T) {
	service, err := deviceservice.New(filepath.Join(t.TempDir(), "devices.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/p2p/signal" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"code":"ACCOUNT_REQUIRED","message":"请在被控端登录并绑定账号"}`))
			return
		}
		service.ServeHTTP(w, r)
	}))
	defer server.Close()
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c.ServiceURL = server.URL
	r, err := startEmbedded(context.Background(), c, map[string]provider.Provider{"demo": testagent.New()})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		var info map[string]any
		if err := r.Call(context.Background(), control.Request{Method: "host.info"}, &info); err != nil {
			t.Fatal(err)
		}
		network, _ := info["network"].(map[string]any)
		if network["state"] == "error" {
			if strings.Contains(info["status"].(string), "在线") || !strings.Contains(network["detail"].(string), "登录并绑定") {
				t.Fatalf("incorrect host status: %v", info["status"])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("host never reported the signal rejection")
}

type blockingProvider struct {
	*testagent.Provider
	entered chan struct{}
}

func (p *blockingProvider) Sessions(ctx context.Context, _ string) (provider.SessionPage, error) {
	close(p.entered)
	<-ctx.Done()
	return provider.SessionPage{}, ctx.Err()
}

func TestEmbeddedShutdownCancelsOutstandingCalls(t *testing.T) {
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &blockingProvider{Provider: testagent.New(), entered: make(chan struct{})}
	r, err := startEmbedded(context.Background(), c, map[string]provider.Provider{"demo": p})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	callDone := make(chan error, 1)
	go func() {
		callDone <- r.Call(context.Background(), control.Request{Method: "sessions", Provider: "demo"}, nil)
	}()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider was not called")
	}
	closed := make(chan error, 1)
	go func() { closed <- r.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown waited indefinitely for an in-process request")
	}
	if err := <-callDone; err == nil {
		t.Fatal("in-flight call was not canceled")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalControllerUsesSharedBusinessAndCacheWithoutService(t *testing.T) {
	c, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Refuse every service request. Local Agent management must remain usable.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
	defer server.Close()
	c.ServiceURL = server.URL
	agent := testagent.New()
	runtime, err := startEmbedded(context.Background(), c, map[string]provider.Provider{"demo": agent})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	hub, err := clienthub.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	hub.SetLocal(runtime, "Fixture")
	list, err := hub.List()
	if err != nil || len(list) != 1 || list[0]["id"] != control.LocalTarget || list[0]["connected"] != true {
		t.Fatal(list, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	value, err := hub.Call(ctx, control.LocalTarget, control.Request{Method: "providers"})
	if err != nil || value == nil {
		t.Fatal(value, err)
	}
	_, err = hub.Sync(ctx, control.LocalTarget, "demo")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := hub.Cached(control.LocalTarget, control.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.(map[string]any)["sessions"].([]map[string]any)) != 1 {
		t.Fatal("local directory bypassed shared cache")
	}
	request := control.Request{ID: "fixture-create", Method: "create", Provider: "demo", Options: provider.CreateOptions{CWD: t.TempDir()}}
	created, err := hub.Call(ctx, control.LocalTarget, request)
	if err != nil {
		t.Fatal(err)
	}
	// Both entry paths consult the same durable execution receipt.
	if r := runtime.Dispatcher.HandleRemote(control.WithRemoteIdentity(ctx, "other-controller"), request); r.Error == "" {
		t.Fatal("another controller received the local command receipt")
	}
	repeated := runtime.Dispatcher.HandleRequest(ctx, request)
	if repeated.Error != "" {
		t.Fatal(repeated.Error)
	}
	var again map[string]any
	if err := json.Unmarshal(repeated.Result, &again); err != nil {
		t.Fatal(err)
	}
	id := created.(map[string]any)["id"].(string)
	if again["id"] != id || len(agent.SessionsByID) != 2 {
		t.Fatal("local/remote creation did not share receipt")
	}
	send := control.Request{ID: "fixture-send", Method: "send", Provider: "demo", Session: id, Text: "isolated fixture"}
	if _, err := hub.Call(ctx, control.LocalTarget, send); err != nil {
		t.Fatal(err)
	}
	if response := runtime.Dispatcher.HandleRequest(ctx, send); response.Error != "" {
		t.Fatal(response.Error)
	}
	if agent.Sends != 1 {
		t.Fatal("duplicate command executed")
	}
	transcript, err := hub.Call(ctx, control.LocalTarget, control.Request{Method: "transcript", Provider: "demo", Session: id})
	if err != nil || transcript == nil {
		t.Fatal(transcript, err)
	}
	if response := runtime.Dispatcher.HandleRemote(ctx, control.Request{Method: "host.info"}); response.Error == "" {
		t.Fatal("remote crossed local administration boundary")
	}
	if err := hub.Disconnect(control.LocalTarget); err == nil {
		t.Fatal("local runtime was remotely disconnected")
	}
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Call(ctx, control.Request{Method: "providers"}, nil); err != nil {
		t.Fatal("controller closed application-owned runtime", err)
	}
}
