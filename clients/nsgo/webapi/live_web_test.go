//go:build js && wasm

package webapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

// Opt-in only: use a dedicated testsupport/workerfixture, never a user's host.
func deployedBrowserStability(t *testing.T) {
	origin, code := os.Getenv("REIGNHUB_TEST_WORKER_ORIGIN"), os.Getenv("REIGNHUB_TEST_FIXTURE_CODE")
	if origin == "" || code == "" {
		t.Skip("requires an isolated deployed Worker fixture")
	}
	a := New(&memoryStore{v: map[string][]byte{}})
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = connectivity.WithProgress(ctx, func(p connectivity.Progress) {
		if p.State == "warning" || p.Stage == "connected" {
			t.Log(p.Message, p.Detail)
		}
	})
	v, err := a.Call(ctx, "client.connect", Object{"service": origin, "code": code, "name": "Isolated Web stability test", "remember": true}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	device := arr(v)[0]
	target := str(device, "id")
	statuses, err := a.Call(ctx, "client.presence", nil, "", nil)
	if err != nil || len(arr(statuses)) != 1 || arr(statuses)[0]["online"] != true {
		t.Fatalf("server directory presence via browser Fetch: %v, %v", statuses, err)
	}
	for round := 0; round < 60; round++ {
		if _, err = a.Call(ctx, "client.sync", Object{"provider": "codex-app"}, target, nil); err != nil {
			t.Fatalf("sync %d: %v", round, err)
		}
		if _, err = a.Call(ctx, "client.call", nil, target, Object{"method": "transcript", "provider": "codex-app", "session": "isolated"}); err != nil {
			t.Fatalf("transcript %d: %v", round, err)
		}
		if round%10 == 0 {
			t.Logf("sync %d connected: %s", round, a.client(target).Path())
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}
