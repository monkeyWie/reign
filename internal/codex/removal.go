package codex

import (
	"context"
	"errors"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/rpc"

	"github.com/monkeyWie/reignhub/internal/provider"
)

// Codex's supported removal operation archives the thread. Never delete its
// private files or acquire an App-owned thread's writer just to remove it.
func (p *Provider) RemoveSession(ctx context.Context, id string) error {
	var out struct{ Thread thread }
	if err := p.call(ctx, "thread/read", map[string]any{"threadId": id}, &out); err != nil {
		return err
	}
	if id == "" || out.Thread.ID != id {
		return fmt.Errorf("会话身份无法确认")
	}
	if provider.SessionBusy(out.Thread.session()) {
		return fmt.Errorf("会话正在执行或等待处理，请结束后再移除")
	}
	err := p.call(ctx, "thread/archive", map[string]any{"threadId": id}, nil)
	var rejected *rpc.Error
	if err != nil && !errors.Is(err, provider.ErrUnavailable) && !errors.As(err, &rejected) {
		return &removalOutcomeUnknown{err}
	}
	return err
}

type removalOutcomeUnknown struct{ error }

func (*removalOutcomeUnknown) OutcomeUnknown() bool { return true }
