//go:build !js

// An isolated Agent fixture for validating the actual Worker-to-native path.
// It never reads or opens user Agent applications or sessions.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/host"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/remote"
	"github.com/monkeyWie/reignhub/internal/store"
	"github.com/monkeyWie/reignhub/internal/syncstate"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type fake struct {
	provider.Provider
	mu   sync.Mutex
	text string
	id   string
	log  *provider.EventLog
}

func (f *fake) Status(context.Context) provider.Status {
	return provider.Status{ID: f.id, Name: "Test Agent", State: "ready", Capabilities: provider.Capabilities{Send: true, History: true, Streaming: true}}
}
func (f *fake) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{{ID: "isolated", ProviderID: f.id, Title: "Isolated Worker integration", CWD: "/fixture/project", UpdatedAt: time.Now().Unix(), Status: "idle", Model: "test-model"}}}, nil
}
func (f *fake) Transcript(context.Context, string, string) (provider.Transcript, error) {
	f.mu.Lock()
	text := f.text
	f.mu.Unlock()
	return provider.Transcript{Session: provider.Session{ID: "isolated", Title: "Worker integration", CWD: "/fixture/project", Model: "test-model"}, Turns: []provider.Turn{{ID: "turn", Status: "completed", Items: []provider.Item{{ID: "message", Role: "assistant", Type: "agentMessage", Text: text, Status: "completed"}}}}}, nil
}
func (f *fake) Send(_ context.Context, id, text string) error {
	if id != "isolated" {
		return fmt.Errorf("unknown fixture session")
	}
	f.mu.Lock()
	f.text = text
	f.mu.Unlock()
	f.log.Publish(provider.StreamEvent{SessionID: id, TurnID: "turn", ItemID: "message", Kind: "item", Item: &provider.Item{ID: "message", Role: "assistant", Type: "agentMessage", Text: text, Status: "completed"}})
	return nil
}
func (f *fake) Models(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "test-model", Name: "Test model", Default: true}}, nil
}
func (f *fake) SendWithModel(ctx context.Context, id, text, model string) error {
	return f.Send(ctx, id, text)
}
func (f *fake) Watch(ctx context.Context, id, epoch string, after int64) (provider.StreamBatch, error) {
	return f.log.Watch(ctx, id, epoch, after)
}
func main() {
	if len(os.Args) != 2 {
		panic("pass local isolated Worker origin")
	}
	dir, err := os.MkdirTemp("", "reignhub-worker-fixture-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	c, _ := config.Load(filepath.Join(dir, "host"))
	c.ServiceURL = os.Args[1]
	c.DeviceName = "Worker test host"
	providerID := os.Getenv("REIGNHUB_TEST_PROVIDER")
	if providerID == "" {
		providerID = "fixture"
	}
	f := &fake{id: providerID, text: "# Worker integration\n\nThis is an **isolated test Agent**.\n\n- End-to-end encrypted messages\n- Native Go provider\n- React browser controller", log: provider.NewEventLog()}
	d := &control.Dispatcher{Providers: map[string]provider.Provider{providerID: f}}
	db, err := store.Open(filepath.Join(dir, "fixture.db"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	index := &syncstate.Index{DB: db, Epoch: device.Random()}
	d.Sync = func(ctx context.Context, p provider.Provider, r control.Request) (any, error) {
		return index.Sync(ctx, p, r.Provider, r.Epoch, r.After)
	}
	m := host.New(c, d, func(config.Config) remote.Options { return remote.Options{} })
	d.Local = m.Local
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go m.Run(ctx)
	printed := false
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			info, err := m.Local(ctx, control.Request{Method: "host.info"})
			if err != nil {
				continue
			}
			v := info.(map[string]any)
			if !printed && v["connectionId"] != "" && m.Network().State == "connected" {
				b, _ := json.Marshal(map[string]any{"connectionId": v["connectionId"], "service": os.Args[1]})
				fmt.Println(string(b))
				printed = true
			}
			for _, p := range v["pending"].([]map[string]any) {
				raw, _ := json.Marshal(map[string]any{"id": p["id"], "allow": true})
				_, _ = m.Local(ctx, control.Request{Method: "host.respond", Params: raw})
			}
		}
	}
}
