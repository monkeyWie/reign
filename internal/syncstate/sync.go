package syncstate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/store"
	"github.com/monkeyWie/reignhub/internal/workspace"
)

type Index struct {
	DB    *store.DB
	Epoch string
	mu    sync.Mutex
	gates map[string]*scopeGate
}

func (i *Index) Sync(ctx context.Context, p provider.Provider, scope, epoch string, after int64) (store.Delta, error) {
	gate, release := i.acquire(scope)
	defer release()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return store.Delta{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return store.Delta{}, err
	}
	items := map[string]json.RawMessage{}
	cursor := ""
	seen := map[string]bool{}
	var workspaces workspace.Resolver
	for pages := 0; ; pages++ {
		if pages >= 200 {
			return store.Delta{}, fmt.Errorf("会话目录过大，请缩小范围")
		}
		page, e := p.Sessions(ctx, cursor)
		if e != nil {
			return store.Delta{}, e
		}
		for _, s := range page.Sessions {
			b, e := json.Marshal(workspaces.Session(s))
			if e != nil {
				return store.Delta{}, e
			}
			items[s.ID] = b
		}
		if page.NextCursor == "" {
			break
		}
		if seen[page.NextCursor] {
			return store.Delta{}, fmt.Errorf("invalid provider pagination")
		}
		seen[page.NextCursor] = true
		cursor = page.NextCursor
	}
	if err := ctx.Err(); err != nil {
		return store.Delta{}, err
	}
	if e := i.DB.Replace(scope, items); e != nil {
		return store.Delta{}, e
	}
	if epoch != i.Epoch {
		after = 0
	}
	return i.DB.Delta(scope, i.Epoch, after)
}

// Scope gates serialize snapshots of one directory without holding up another
// Provider. Reference counting removes gates after both scans and waiters leave.
type scopeGate struct {
	ready chan struct{}
	refs  int
}

func (i *Index) acquire(scope string) (chan struct{}, func()) {
	i.mu.Lock()
	if i.gates == nil {
		i.gates = make(map[string]*scopeGate)
	}
	g := i.gates[scope]
	if g == nil {
		g = &scopeGate{ready: make(chan struct{}, 1)}
		i.gates[scope] = g
	}
	g.refs++
	i.mu.Unlock()
	return g.ready, func() {
		i.mu.Lock()
		defer i.mu.Unlock()
		g.refs--
		if g.refs == 0 {
			delete(i.gates, scope)
		}
	}
}
