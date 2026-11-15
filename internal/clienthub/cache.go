package clienthub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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
			var snapshot any
			err := h.DB.Get(snapshotKey(id, r.Provider, r.Session), &snapshot)
			if err == nil {
				return snapshot, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		raw, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		var out any
		err = h.DB.Get("rpc:"+id+":"+string(raw), &out)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return out, err
	}
	providers, err := h.Cached(id, control.Request{Method: "providers"})
	if err != nil {
		return nil, err
	}
	keys, err := h.DB.Keys("directory:" + id + ":")
	if err != nil {
		return nil, err
	}
	sessions := []map[string]any{}
	directories := map[string]bool{}
	for _, key := range keys {
		var directory struct{ Items map[string]map[string]any }
		if err = h.DB.Get(key, &directory); err != nil {
			return nil, err
		}
		directories[key] = true
		for _, session := range directory.Items {
			sessions = append(sessions, session)
		}
	}
	// A first metadata page is already persisted by Call. Use it while the
	// authoritative directory scan is still in flight, including after restart.
	var statuses []struct {
		ID string `json:"id"`
	}
	raw, err := json.Marshal(providers)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &statuses); err != nil {
		return nil, err
	}
	for _, p := range statuses {
		if directories["directory:"+id+":"+p.ID] {
			continue
		}
		page, err := h.Cached(id, control.Request{Method: "sessions", Provider: p.ID})
		if err != nil {
			return nil, err
		}
		if page == nil {
			continue
		}
		raw, err := json.Marshal(page)
		if err != nil {
			return nil, err
		}
		var data struct {
			Sessions []map[string]any `json:"sessions"`
		}
		if err = json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		sessions = append(sessions, data.Sessions...)
	}
	return map[string]any{"providers": providers, "sessions": sessions}, nil
}

func snapshotKey(id, provider, session string) string {
	b, _ := json.Marshal([]string{provider, session})
	return "snapshot:" + id + ":" + string(b)
}

// SaveSnapshot persists the rendered projection, including deltas received
// since the last remote snapshot. This is local data, never an Agent command.
func (h *Hub) SaveSnapshot(id, provider, session string, snapshot json.RawMessage) error {
	if id == "" || provider == "" || session == "" || !json.Valid(snapshot) {
		return fmt.Errorf("invalid local transcript snapshot")
	}
	return h.DB.Put(snapshotKey(id, provider, session), snapshot)
}
