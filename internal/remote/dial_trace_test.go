package remote

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

func TestDialTraceIdentifiesWaitingForUpgradeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	steps := []connectivity.Progress{}
	ctx = connectivity.WithProgress(ctx, func(p connectivity.Progress) { mu.Lock(); steps = append(steps, p); mu.Unlock() })
	_, _, err := dialWebSocket(ctx, server.URL, "private-token", "signal")
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "等待服务响应") {
		t.Fatalf("lost stalled phase: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, step := range steps {
		if strings.Contains(step.Detail, "private-token") {
			t.Fatal("trace disclosed auth")
		}
		if step.ID == "signal:dial:1:http-upgrade" && step.State == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatal("timeout dropped its final trace event")
	}
}
func TestDialTraceIdentifiesTLSFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _, err := dialWebSocket(ctx, server.URL, "private-token", "signal")
	if err == nil || !strings.Contains(err.Error(), "TLS") || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("lost TLS failure phase: %v", err)
	}
}
