// Test-only host. It never discovers an installed Agent or invokes a model.
package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/monkeyWie/reign/internal/assets"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/relay"
	"github.com/monkeyWie/reign/internal/remote"
)

type fixture struct {
	provider.Provider
	sends atomic.Int32
}

func (f *fixture) Status(context.Context) provider.Status {
	return provider.Status{ID: "fixture", Name: "Test Agent", State: "ready"}
}
func (f *fixture) Sessions(context.Context, string) (provider.SessionPage, error) {
	title := "fixture"
	if f.sends.Load() > 0 {
		title = "received-from-flutter"
	}
	return provider.SessionPage{Sessions: []provider.Session{{ID: "fixture-session", Title: title, Preview: strings.Repeat("互通", 150000)}}}, nil
}
func (f *fixture) Send(_ context.Context, id, text string) error {
	if id != "fixture-session" || text != "from-flutter" {
		return fmt.Errorf("unexpected fixture instruction")
	}
	f.sends.Add(1)
	return nil
}
func (f *fixture) SendAttachments(context.Context, string, string, string, []string) error {
	return fmt.Errorf("not used by this fixture")
}
func (f *fixture) SteerAttachments(context.Context, string, string, string, []string) error {
	return fmt.Errorf("not used by this fixture")
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	token := strings.Repeat("test-only-", 4)
	endpoint := os.Getenv("AGENTHUB_TEST_RELAY_URL")
	if endpoint == "" {
		r := relay.New(token)
		r.Origins = []string{"http://localhost:*", "http://127.0.0.1:*"}
		server := httptest.NewServer(r)
		defer server.Close()
		endpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	} else {
		token = os.Getenv("AGENTHUB_TEST_RELAY_TOKEN")
	}
	p := remote.Pairing{Relay: endpoint, Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	status := remote.NewHostStatus(endpoint)
	dir, e := os.MkdirTemp("", "attachment-relay-fixture-*")
	if e != nil {
		panic(e)
	}
	defer os.RemoveAll(dir)
	d := &control.Dispatcher{Assets: assets.New(dir), Providers: map[string]provider.Provider{"fixture": &fixture{}}, Network: status.Snapshot}
	go remote.RunHostObserved(ctx, p, remote.Options{}, d, status)
	deadline := time.Now().Add(15 * time.Second)
	for status.Snapshot().State != "connected" {
		if time.Now().After(deadline) {
			panic("fixture could not reach relay")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(25 * time.Millisecond):
		}
	}
	fmt.Println(p.Code())
	<-ctx.Done()
}
