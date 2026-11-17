// Package readretry controls retries of idempotent synchronization reads only.
// Command submission and approval responses must never use this policy.
package readretry

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"strings"
	"time"
)

type Backoff struct{ failures int }

func (b *Backoff) Reset() { b.failures = 0 }

// Next is scoped to one session and operation. Persistent data/schema errors
// probe infrequently while other sessions, catalogue reads and live output run.
func (b *Backoff) Next(err error) time.Duration {
	if err == nil {
		b.Reset()
		return 0
	}
	b.failures = min(b.failures+1, 12)
	base, cap := time.Second, 10*time.Second
	text := strings.ToLower(err.Error())
	switch {
	case contains(text, "unsupported", "未知", "不支持", "invalid", "decode", "解析", "超过", "too large", "不连续", "内容冲突"):
		base, cap = 15*time.Second, 2*time.Minute
	case contains(text, "active writer", "持有", "busy", "忙碌", "rate limit", "429"):
		base, cap = 2*time.Second, 30*time.Second
	default:
		var network net.Error
		transient := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.As(err, &network) || contains(text, "connection", "连接", "network", "deadline", "timeout", "离线", "unavailable", "断开")
		if !transient {
			base, cap = 5*time.Second, time.Minute
		}
	}
	delay := min(base*time.Duration(1<<min(b.failures-1, 7)), cap)
	// Bounded jitter prevents synchronized reconnecting clients hammering a host.
	return delay*4/5 + time.Duration(rand.Int64N(int64(delay/5)+1))
}
func contains(s string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}
func Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
