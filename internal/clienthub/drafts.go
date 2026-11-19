package clienthub

import (
	"context"
	"database/sql"
	"errors"

	"github.com/monkeyWie/reignhub/internal/store"
	"github.com/monkeyWie/reignhub/pkg/composerdraft"
)

type draftStore struct{ *store.DB }

func (s draftStore) Get(key string) (any, error) {
	var v any
	err := s.DB.Get(key, &v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}
func (s draftStore) Keys() ([]string, error) { return s.DB.Keys("draft:") }
func (h *Hub) Draft(ctx context.Context, method, target string, p map[string]any) (any, error) {
	return composerdraft.Call(ctx, draftStore{h.DB}, method, target, p)
}
