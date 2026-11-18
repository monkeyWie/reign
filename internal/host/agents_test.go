package host

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/testagent"
)

type automaticFixture struct {
	*testagent.Provider
	entered   chan struct{}
	cancelled chan struct{}
}

func (p *automaticFixture) AutoConnect(ctx context.Context) error {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	if p.cancelled != nil {
		<-ctx.Done()
		select {
		case p.cancelled <- struct{}{}:
		default:
		}
		return ctx.Err()
	}
	return nil
}

func TestAgentDefaultsIndependentStartupAndPersistentSwitch(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	slow := &automaticFixture{testagent.New(), make(chan struct{}, 2), make(chan struct{}, 1)}
	fast := &automaticFixture{testagent.New(), make(chan struct{}, 2), nil}
	d := &control.Dispatcher{Providers: map[string]provider.Provider{"slow": slow, "fast": fast}}
	m := New(cfg, d, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.autoConnect(ctx) }()
	defer func() { cancel(); <-done }()
	for _, p := range []*automaticFixture{slow, fast} {
		select {
		case <-p.entered:
		case <-time.After(time.Second):
			t.Fatal("an Agent blocked another's default startup")
		}
	}
	params := json.RawMessage(`{"provider":"slow","enabled":false}`)
	if _, err := m.Local(ctx, control.Request{Method: "host.agent.enabled", Params: params}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-slow.cancelled:
	case <-time.After(time.Second):
		t.Fatal("switch did not cancel pending setup")
	}
	if m.agentEnabled("slow") || !m.agentEnabled("fast") {
		t.Fatal("switch affected another Agent")
	}
	for _, invoke := range []func(context.Context, control.Request) control.Response{d.HandleRequest, d.HandleRemote} {
		if got := invoke(ctx, control.Request{Method: "create", Provider: "slow"}); got.Error == "" {
			t.Fatal("disabled Agent accepted new work")
		}
	}
	if got := d.HandleRemote(ctx, control.Request{Method: "host.agent.enabled", Params: params}); got.Error == "" {
		t.Fatal("remote client changed local preference")
	}
	saved, err := config.Load(cfg.DataDir)
	if err != nil || saved.AgentIsEnabled("slow") || !saved.AgentIsEnabled("fast") {
		t.Fatal("preference not persisted", err)
	}
	if _, err := m.Local(ctx, control.Request{Method: "host.agent.enabled", Params: json.RawMessage(`{"provider":"slow","enabled":true}`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-slow.entered:
	case <-time.After(time.Second):
		t.Fatal("enable did not resume attachment")
	}
}
