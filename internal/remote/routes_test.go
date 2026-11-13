package remote

import (
	"context"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/relay"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudRouteFallbackWithoutReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	fake := &testProvider{log: provider.NewEventLog()}
	d := &control.Dispatcher{Providers: map[string]provider.Provider{"test": fake}}
	makeRoute := func() (Pairing, context.CancelFunc) {
		token := strings.Repeat("r", 32)
		server := httptest.NewServer(relay.New(token))
		t.Cleanup(server.Close)
		p := Pairing{Relay: "ws" + strings.TrimPrefix(server.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
		life, stop := context.WithCancel(ctx)
		go RunHostObserved(life, p, Options{RelayOnly: true}, d, NewHostStatus(p.Relay))
		return p, stop
	}
	primary, stopPrimary := makeRoute()
	defer stopPrimary()
	cloud, stopCloud := makeRoute()
	defer stopCloud()
	c, err := ConnectRoutes(ctx, primary, cloud, Options{RelayOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitFor(t, func() bool { return c.Path() == "Relay · Cloudflare Tunnel" })
	if err = c.Call(ctx, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil {
		t.Fatal(err)
	}
	stopCloud()
	waitFor(t, func() bool { return c.Path() == "Relay · encrypted" })
	if err = c.Call(ctx, control.Request{Method: "send", Provider: "test", Session: "original-thread", Text: "continue"}, nil); err != nil {
		t.Fatal(err)
	}
	if fake.sends.Load() != 2 {
		t.Fatal("route change replayed a write", fake.sends.Load())
	}
	c.Close()
	waitFor(t, func() bool { return c.Path() == "Disconnected" })
}
