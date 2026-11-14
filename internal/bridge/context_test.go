package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCallContextCancelsInFlightReadWithoutShuttingDownHub(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if h := hubs[dir]; h != nil {
			h.Close()
			delete(hubs, dir)
		}
	})
	raw, _ := json.Marshal(map[string]any{"method": "client.connect", "dataDir": dir, "params": map[string]any{"service": server.URL, "code": "123456"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() { done <- CallContext(ctx, string(raw)) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request never started")
	}
	cancel()
	select {
	case response := <-done:
		if !strings.Contains(response, "context canceled") {
			t.Fatalf("unexpected cancellation: %s", response)
		}
	case <-time.After(time.Second):
		t.Fatal("native bridge ignored caller cancellation")
	}
	list, _ := json.Marshal(map[string]any{"method": "client.list", "dataDir": dir})
	if response := CallContext(context.Background(), string(list)); strings.Contains(response, `"error"`) {
		t.Fatalf("cancellation stopped shared hub: %s", response)
	}
}
