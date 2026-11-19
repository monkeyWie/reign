//go:build (darwin && !ios) || (linux && !android) || windows || freebsd || openbsd || netbsd || dragonfly

package codex

import (
	"context"
	"testing"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestCodexReadinessDoesNotRequireDesktopDiscovery(t *testing.T) {
	p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) { t.Errorf("status invoked %s", m.Method); return nil, nil })
	attachIPCFixture(t, p, nil)
	// Cancel OS metadata discovery. The already healthy transports remain the
	// authority for readiness, even without a desktop process observation.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := p.Status(ctx)
	if !s.Ready() || !s.Running || s.ID != "codex-app" || s.Name != "Codex" {
		t.Fatalf("healthy unified connection depends on App discovery: %+v", s)
	}
	if err := p.AutoConnect(ctx); err != nil {
		t.Fatalf("healthy connection tried to reconnect: %v", err)
	}
}
