package clienthub

import (
	"context"

	"encoding/json"

	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"

	"github.com/monkeyWie/reignhub/internal/control"
)

// Cached never touches a transport. A connected but slow peer must not delay
// opening locally stored sessions or messages.
func (h *Hub) Cached(id string, r control.Request) (any, error) {
	if r.Method != "" {
		if !control.ReadOnly(r.Method) {
			return nil, fmt.Errorf("only read snapshots can be loaded from cache")
		}
		if r.Method == "transcript" && r.Cursor == "" {
			page, err := h.DB.ReadPage(context.Background(), persistence.Session{Env: id, Provider: r.Provider, ID: r.Session}, persistence.PageQuery{})
			snapshot := page.View()
			if err != nil {
				return nil, err
			}
			if snapshot != nil {
				return snapshot, nil
			}
		}
		rows, e := h.DB.LoadList(context.Background(), persistence.Session{Env: id, Provider: r.Provider, ID: r.Session}, persistence.ReadKind(r.Method))
		if rows == nil {
			return nil, e
		}
		return rows, e
	}
	providers, err := h.Cached(id, control.Request{Method: "providers"})
	if err != nil {
		return nil, err
	}
	sessions, err := h.DB.ListSessions(context.Background(), persistence.DirectoryKey{Env: id})
	if err != nil {
		return nil, err
	}
	return map[string]any{"providers": providers, "sessions": sessions}, nil
}

func (h *Hub) SaveSnapshot(id, provider, session string, snapshot json.RawMessage) error {
	var value persistence.Object
	if err := json.Unmarshal(snapshot, &value); err != nil {
		return err
	}
	return h.DB.ApplyHistory(context.Background(), persistence.Session{Env: id, Provider: provider, ID: session}, persistence.Diff("", nil, value))
}
