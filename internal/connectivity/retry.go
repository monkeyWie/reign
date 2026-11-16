package connectivity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"
)

const (
	// Setup budgets never apply to established business RPCs.
	SetupAttemptTimeout  = 30 * time.Second
	SocketAttemptTimeout = 10 * time.Second
	ResumeTimeout        = 2 * time.Second
	ApprovalTimeout      = 2 * time.Minute
	SetupAttempts        = 3
)

// RetryableSetup is for establishing an authenticated connection only. Never
// use it to replay a command, approval request, or an application rejection.
func RetryableSetup(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var e net.Error
	return errors.As(err, &e) && (e.Timeout() || e.Temporary())
}

// RetrySetup replaces a stalled setup with a fresh connection. Each attempt has
// its own short budget. A first user connection ends after this bounded burst.
func RetrySetup[T any](ctx context.Context, connect func(context.Context) (T, error)) (T, error) {
	return retrySetup(ctx, SetupAttemptTimeout, SetupAttempts, connect)
}
func retrySetup[T any](ctx context.Context, budget time.Duration, attempts int, connect func(context.Context) (T, error)) (T, error) {
	var value T
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			return value, ctx.Err()
		}
		setup, stop := context.WithTimeout(ctx, budget)
		value, err = connect(setup)
		stop()
		if err == nil || ctx.Err() != nil || !RetryableSetup(err) || attempt == attempts {
			if ctx.Err() != nil {
				return value, ctx.Err()
			}
			return value, err
		}
		Note(ctx, fmt.Sprintf("setup-retry:%d", attempt), "retry", "当前连接未能就绪，正在快速重试", fmt.Sprintf("第 %d/%d 次 · %v", attempt+1, attempts, err), "warning")
		if err = RetryPause(ctx, attempt); err != nil {
			return value, err
		}
	}
	return value, err
}

func RetryPause(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
