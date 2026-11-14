// Package testagent is a deterministic, local-only Agent fixture for integration tests.
// It never starts a model, reads Codex data or executes commands.
package testagent

import (
	"context"
	"fmt"
	"sync"

	"github.com/monkeyWie/reignhub/internal/assets"
	"github.com/monkeyWie/reignhub/internal/provider"
)

type Provider struct {
	mu           sync.Mutex
	SessionsByID map[string]provider.Session
	Turns        map[string][]provider.Turn
	Sends        int
	Assets       *assets.Store
}

func New(stores ...*assets.Store) *Provider {
	var store *assets.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	return &Provider{Assets: store, SessionsByID: map[string]provider.Session{"existing": {ID: "existing", ProviderID: "demo", Title: "Demo · 现有会话", Status: "idle", UpdatedAt: 1}}, Turns: map[string][]provider.Turn{"existing": {{ID: "welcome", Status: "completed", Items: []provider.Item{{ID: "hello", Role: "assistant", Text: "这是模拟 Agent，用于验证连接、配对和会话同步。不会调用真实模型。"}}}}}}
}
func (p *Provider) Status(context.Context) provider.Status {
	return provider.Status{ID: "demo", Name: "Demo Agent", State: "ready", Installed: true, Running: true, Capabilities: provider.Capabilities{History: true, Create: true, Send: true, Steer: true, Interrupt: true, Approvals: true}}
}
func (p *Provider) Start(context.Context) error { return nil }
func (p *Provider) Close() error                { return nil }
func (p *Provider) Sessions(context.Context, string) (provider.SessionPage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := provider.SessionPage{Sessions: []provider.Session{}}
	for _, s := range p.SessionsByID {
		out.Sessions = append(out.Sessions, s)
	}
	return out, nil
}
func (p *Provider) Transcript(_ context.Context, id, cursor string) (provider.Transcript, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.SessionsByID[id]
	if !ok {
		return provider.Transcript{}, fmt.Errorf("session not found")
	}
	return provider.Transcript{Session: s, Turns: append([]provider.Turn{}, p.Turns[id]...)}, nil
}
func (p *Provider) Create(_ context.Context, o provider.CreateOptions) (provider.Session, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := fmt.Sprint("new-", len(p.SessionsByID))
	s := provider.Session{ID: id, ProviderID: "demo", Title: "Demo · 新会话", Status: "idle", CWD: o.CWD, UpdatedAt: 2}
	p.SessionsByID[id] = s
	return s, nil
}
func (p *Provider) Send(_ context.Context, id, text string) error { return p.send(id, text, nil) }
func (p *Provider) send(id, text string, attachments []provider.Attachment) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s, ok := p.SessionsByID[id]
	if !ok {
		return fmt.Errorf("session not found")
	}
	p.Sends++
	p.Turns[id] = append(p.Turns[id], provider.Turn{ID: fmt.Sprint(p.Sends), Status: "completed", Items: []provider.Item{{ID: fmt.Sprint("user", p.Sends), Role: "user", Text: text, Attachments: attachments}, {ID: fmt.Sprint("reply", p.Sends), Role: "assistant", Text: "Demo echo: " + text}}})
	s.UpdatedAt++
	p.SessionsByID[id] = s
	return nil
}
func (p *Provider) Steer(ctx context.Context, id, turn, text string) error {
	return p.Send(ctx, id, text)
}
func (p *Provider) Interrupt(context.Context, string, string) error                 { return nil }
func (p *Provider) Approvals() []provider.Approval                                  { return []provider.Approval{} }
func (p *Provider) Decide(context.Context, string, provider.Decision) error         { return nil }
func (p *Provider) ConnectDesktop(context.Context, []provider.Instance, bool) error { return nil }

func (p *Provider) SendAttachments(ctx context.Context, id, text, model string, ids []string) error {
	if p.Assets == nil {
		return fmt.Errorf("fixture attachments unavailable")
	}
	metas := []provider.Attachment{}
	for _, assetID := range ids {
		meta, _, e := p.Assets.Input(assets.Scope("demo", id), assetID)
		if e != nil {
			return e
		}
		metas = append(metas, meta)
	}
	return p.send(id, text, metas)
}
func (p *Provider) SteerAttachments(ctx context.Context, id, turn, text string, ids []string) error {
	return p.SendAttachments(ctx, id, text, "", ids)
}
