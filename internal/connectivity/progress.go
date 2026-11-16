package connectivity

import (
	"context"
	"time"
)

// Progress describes an actual connection step, without credentials or payloads.
// The observer runs on the networking goroutine; UIs must dispatch to their UI loop.
type Progress struct {
	ID      string
	Detail  string
	State   string // running, success, info, warning
	Stage   string
	Message string
	Timeout time.Duration
}

// StageTimeout is a fresh budget for each connection stage, never a total attempt budget.
const StageTimeout = 30 * time.Second

type progressKey struct{}

func WithProgress(ctx context.Context, observe func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, observe)
}

func Report(ctx context.Context, stage, message string, timeout time.Duration) {
	if ctx.Err() != nil {
		return
	}
	if deadline, ok := ctx.Deadline(); ok && (timeout == 0 || time.Until(deadline) < timeout) {
		timeout = max(0, time.Until(deadline))
	}
	if observe := Observer(ctx); observe != nil {
		observe(Progress{ID: stage, Stage: stage, Message: message, Timeout: timeout, State: "running"})
	}
}

// Observer survives the initial handshake context; the wire owns its lifetime.
func Observer(ctx context.Context) func(Progress) {
	observe, _ := ctx.Value(progressKey{}).(func(Progress))
	return observe
}
func Note(ctx context.Context, id, stage, message, detail, state string) {
	if ctx.Err() != nil {
		return
	}
	if observe := Observer(ctx); observe != nil {
		observe(Progress{ID: id, Stage: stage, Message: message, Detail: detail, State: state})
	}
}
