package host_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/clienthub"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/host"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/remote"
	"github.com/monkeyWie/reignhub/internal/testagent"
)

// Explicitly opt in to testing a deployed Worker. Both identities are isolated
// and the host exposes only a fake provider, never installed Agent sessions.
func TestDeployedWorkerNativeRelay(t *testing.T) {
	origin := os.Getenv("REIGNHUB_TEST_WORKER_ORIGIN")
	if origin == "" {
		t.Skip("set REIGNHUB_TEST_WORKER_ORIGIN to test a deployed Worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ServiceURL, cfg.DeviceName = origin, "Isolated relay test host"
	d := &control.Dispatcher{Providers: map[string]provider.Provider{"codex-cli": testagent.New()}}
	m := host.New(cfg, d, func(config.Config) remote.Options { return remote.Options{RelayOnly: true} })
	done := make(chan struct{})
	go func() { defer close(done); m.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for m.Network().State != "connected" {
		select {
		case <-ctx.Done():
			t.Fatalf("host: %+v", m.Network())
		case <-time.After(100 * time.Millisecond):
		}
	}
	info := invoke(t, m, "host.info", nil).(map[string]any)
	invoke(t, m, "host.password", map[string]any{"password": "isolated-fixture-password"})
	hub, err := clienthub.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer hub.Close()
	trace := connectivity.WithProgress(ctx, func(p connectivity.Progress) { t.Log(p.Stage, p.Message, p.Detail) })
	saved, err := hub.Connect(trace, origin, info["connectionId"].(string), info["public"].(string), "isolated-fixture-password", "Isolated relay test controller", true, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []control.Request{{Method: "providers"}, {Method: "sessions", Provider: "codex-cli"}} {
		result, err := hub.Call(ctx, saved.ID, req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		t.Log(req.Method, "response bytes:", len(raw))
	}
}
