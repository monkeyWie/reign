package connectivity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSetupRetriesReplaceStalledConnectionAndRemainBounded(t *testing.T) {
	var attempts int
	started := time.Now()
	value, err := retrySetup(context.Background(), 20*time.Millisecond, 3, func(ctx context.Context) (string, error) {
		attempts++
		if attempts == 2 {
			return "connected", nil
		}
		<-ctx.Done()
		return "", ctx.Err()
	})
	if err != nil || value != "connected" || attempts != 2 || time.Since(started) > 2*time.Second {
		t.Fatalf("stalled setup was not replaced: %q %d %v", value, attempts, err)
	}
	attempts = 0
	_, err = retrySetup(context.Background(), 10*time.Millisecond, 3, func(ctx context.Context) (string, error) {
		attempts++
		<-ctx.Done()
		return "", ctx.Err()
	})
	if attempts != 3 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded setup burst: %d %v", attempts, err)
	}
}

func TestSetupRejectionsAndCancellationNeverRetry(t *testing.T) {
	rejection := errors.New("HTTP 403: pairing revoked")
	var attempts int
	_, err := retrySetup(context.Background(), time.Second, 3, func(context.Context) (string, error) {
		attempts++
		return "", rejection
	})
	if attempts != 1 || !errors.Is(err, rejection) {
		t.Fatal("authorization rejection retried", attempts, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	attempts = 0
	_, err = retrySetup(ctx, time.Second, 3, func(context.Context) (string, error) {
		attempts++
		cancel()
		return "", context.DeadlineExceeded
	})
	if attempts != 1 || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation retried", attempts, err)
	}
}
