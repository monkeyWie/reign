package app

import (
	"context"
	"net"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/deviceservice"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/testagent"
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
		if info["connectionId"] != "" && info["status"] == "在线" {
			return info
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("embedded registration timed out")
	return nil
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
		t.Fatal("shutdown waited indefinitely for an FFI request")
	}
	if err := <-callDone; err == nil {
		t.Fatal("in-flight call was not canceled")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}
