package readretry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReadFailureBackoffIsolationAndRecovery(t *testing.T) {
	var busy, live Backoff
	for i := 0; i < 20; i++ {
		if delay := busy.Next(errors.New("decode unsupported schema")); delay < 12*time.Second || delay > 2*time.Minute {
			t.Fatal(delay)
		}
	}
	if delay := live.Next(context.DeadlineExceeded); delay < 800*time.Millisecond || delay > time.Second {
		t.Fatal("another session inherited quarantine", delay)
	}
	busy.Next(nil)
	if delay := busy.Next(context.DeadlineExceeded); delay > time.Second {
		t.Fatal("success did not reset backoff", delay)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wait(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
