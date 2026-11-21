package clienthub

import (
	"context"
	"github.com/monkeyWie/reignhub/pkg/composerdraft"
)

func (h *Hub) Draft(ctx context.Context, method, target string, p map[string]any) (any, error) {
	return composerdraft.Dispatch(ctx, h.DB, method, target, p)
}
