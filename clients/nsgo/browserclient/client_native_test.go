//go:build !js

package browserclient

import (
	"context"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/relay"
	"github.com/monkeyWie/reign/internal/remote"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureProvider struct {
	provider.Provider
	sends atomic.Int32
}

func (*fixtureProvider) Status(context.Context) provider.Status {
	return provider.Status{ID: "fixture", State: "ready"}
}
func (*fixtureProvider) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{{ID: "fixture-session", Preview: strings.Repeat("跨端消息🙂", 20000)}}}, nil
}
func (p *fixtureProvider) Send(context.Context, string, string) error { p.sends.Add(1); return nil }
func TestLegacyHostEncryptedInterop(t *testing.T) {
	token := strings.Repeat("r", 32)
	srv := httptest.NewServer(relay.New(token))
	defer srv.Close()
	p := remote.Pairing{Relay: "ws" + strings.TrimPrefix(srv.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor := remote.NewHostStatus(p.Relay)
	fake := &fixtureProvider{}
	go remote.RunHostObserved(ctx, p, remote.Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake}}, monitor)
	deadline := time.Now().Add(5 * time.Second)
	for monitor.Snapshot().State != "connected" {
		if time.Now().After(deadline) {
			t.Fatal("fixture host did not connect")
		}
		time.Sleep(time.Millisecond)
	}
	c, e := Connect(ctx, Pairing{Relay: p.Relay, Host: p.Host, Key: p.Key, Token: token, RelayOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	for range 2 {
		v, e := c.Call(ctx, Object{"method": "sessions", "provider": "fixture"})
		if e != nil {
			t.Fatal(e)
		}
		rows := v.(map[string]any)["sessions"].([]any)
		if len(rows) != 1 || len(rows[0].(map[string]any)["preview"].(string)) < 100000 {
			t.Fatal("large fragmented transcript lost bytes")
		}
	}
	if _, e = c.Call(ctx, Object{"method": "send", "provider": "fixture", "session": "fixture-session", "text": "test fixture only"}); e != nil || fake.sends.Load() != 1 {
		t.Fatalf("mutation sent %d times: %v", fake.sends.Load(), e)
	}
	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for c.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("host disconnect was not observed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAlternateRouteDiscoveryAndFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fake := &fixtureProvider{}
	makeRoute := func() (Pairing, context.CancelFunc) {
		token := strings.Repeat("r", 32)
		srv := httptest.NewServer(relay.New(token))
		t.Cleanup(srv.Close)
		p := Pairing{Relay: "ws" + strings.TrimPrefix(srv.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), Token: token, RelayOnly: true}
		life, stop := context.WithCancel(ctx)
		go remote.RunHostObserved(life, remote.Pairing{Relay: p.Relay, Host: p.Host, Key: p.Key, RelayToken: token}, remote.Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake}}, remote.NewHostStatus(p.Relay))
		return p, stop
	}
	primary, stopPrimary := makeRoute()
	defer stopPrimary()
	cloud, stopCloud := makeRoute()
	defer stopCloud()
	primary.DiscoverTunnel = func(context.Context) (Pairing, error) { return cloud, nil }
	c, err := ConnectRoutes(ctx, primary, Pairing{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	await := func(fn func() bool) {
		t.Helper()
		deadline := time.Now().Add(7 * time.Second)
		for !fn() {
			if time.Now().After(deadline) {
				t.Fatal("route failed to settle", c.Path())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	await(func() bool { return strings.Contains(c.Path(), "Cloudflare Tunnel") })
	if _, err = c.Call(ctx, Object{"method": "send", "provider": "fixture", "session": "fixture-session", "text": "fixture"}); err != nil {
		t.Fatal(err)
	}
	stopCloud()
	await(func() bool { return c.Connected() && !strings.Contains(c.Path(), "Cloudflare Tunnel") })
	if _, err = c.Call(ctx, Object{"method": "send", "provider": "fixture", "session": "fixture-session", "text": "fixture"}); err != nil {
		t.Fatal(err)
	}
	if fake.sends.Load() != 2 {
		t.Fatal("route fallback replayed writes", fake.sends.Load())
	}
	c.Close()
	await(func() bool { return !c.Connected() })
}
