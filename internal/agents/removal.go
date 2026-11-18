package agents

import (
	"context"
	"errors"
	"maps"

	"github.com/monkeyWie/reignhub/pkg/localstate"
)

// ACP v1 and Pi have no portable native deletion command. Remove from this
// Provider's durable directory and retain the Agent's history. Tombstones also
// prevent native discovery from reimporting a removed conversation on restart.
func (p *Provider) RemoveSession(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.removeMu.Lock()
	defer p.removeMu.Unlock()
	p.mu.Lock()
	s := p.sessions[id]
	if s == nil {
		p.mu.Unlock()
		return errors.New("会话不存在")
	}
	if p.removed[id] {
		p.mu.Unlock()
		return nil
	}
	if s.sending || s.opening {
		p.mu.Unlock()
		return errors.New("会话正在执行或恢复，请结束后再移除")
	}
	// Block a concurrent send/restore while persistence is in flight, without
	// holding the Provider lock over disk or process operations.
	s.removing = true
	next := maps.Clone(p.removed)
	if next == nil {
		next = map[string]bool{}
	}
	next[id] = true
	p.mu.Unlock()
	err := localstate.Put(p.path(), p.id+":removed", next)
	p.mu.Lock()
	s.removing = false
	var rt runtimeAgent
	if err == nil {
		p.removed = next
		rt = s.runtime
		s.runtime = nil
	}
	p.mu.Unlock()
	if rt != nil {
		_ = rt.Close()
	}
	return err
}
