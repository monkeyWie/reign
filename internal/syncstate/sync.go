package syncstate

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/store"
	"sync"
)

type Index struct {
	DB    *store.DB
	Epoch string
	mu    sync.Mutex
}

func (i *Index) Sync(ctx context.Context, p provider.Provider, scope, epoch string, after int64) (store.Delta, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	items := map[string]json.RawMessage{}
	cursor := ""
	seen := map[string]bool{}
	for pages := 0; ; pages++ {
		if pages >= 200 {
			return store.Delta{}, fmt.Errorf("会话目录过大，请缩小范围")
		}
		page, e := p.Sessions(ctx, cursor)
		if e != nil {
			return store.Delta{}, e
		}
		for _, s := range page.Sessions {
			b, e := json.Marshal(s)
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
	if e := i.DB.Replace(scope, items); e != nil {
		return store.Delta{}, e
	}
	if epoch != i.Epoch {
		after = 0
	}
	return i.DB.Delta(scope, i.Epoch, after)
}
