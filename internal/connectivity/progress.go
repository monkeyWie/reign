package connectivity

import (
	"context"
	"time"
)

// Progress describes an actual connection step, without credentials or payloads.
// The observer runs on the networking goroutine; UIs must dispatch to their UI loop.
type Progress struct {
	Stage   string
	Message string
	Timeout time.Duration
}

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
	if observe, ok := ctx.Value(progressKey{}).(func(Progress)); ok && observe != nil {
		observe(Progress{Stage: stage, Message: message, Timeout: timeout})
	}
}
