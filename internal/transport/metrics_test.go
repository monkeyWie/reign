package transport

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPassiveMetricsExcludeWatchesAndFailedResponses(t *testing.T) {
	var m Meter
	initial := m.Snapshot()
	if _, ok := initial["responseMs"]; ok || initial["lastProgressMs"] != int64(-1) {
		t.Fatal("fabricated measurements", initial)
	}
	m.Connected()
	m.Sent(4096)
	m.Received(2048)
	m.Progress()
	finish := m.Begin("transcript")
	watch := m.Begin("watch")
	cancelled := m.Begin("models")
	if m.Snapshot()["pendingRequests"] != 2 {
		t.Fatal("watch counted as blocked RPC")
	}
	cancelled(context.Canceled)
	finish(nil)
	watch(nil)
	failure := m.Begin("sync")
	failure(errors.New("timeout"))
	got := m.Snapshot()
	if got["sentBytes"] != int64(4096) || got["receivedBytes"] != int64(2048) || got["pendingRequests"] != 0 {
		t.Fatal("bad passive measurements", got)
	}
	if _, ok := got["requestFailurePercent"]; ok {
		t.Fatal("application errors exposed as a network failure rate", got)
	}
	if _, ok := got["responseMs"]; !ok {
		t.Fatal("successful request did not provide response latency", got)
	}
	lastResponse := m.lastResponse
	failure = m.Begin("sync")
	failure(context.DeadlineExceeded)
	if m.lastResponse != lastResponse || m.Snapshot()["pendingRequests"] != 0 {
		t.Fatal("failed request changed successful response sample or stayed pending")
	}
	// Old traffic must expire without any timer, polling goroutine or request.
	m.mu.Lock()
	for n := range m.buckets {
		m.buckets[n].second = time.Now().Add(-10 * time.Second).Unix()
	}
	m.mu.Unlock()
	got = m.Snapshot()
	if got["uploadBps"] != float64(0) || got["downloadBps"] != float64(0) {
		t.Fatal("stale throughput", got)
	}
}
func TestPassiveMetricsConcurrentFramesAndSnapshots(t *testing.T) {
	var m Meter
	m.Connected()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				end := m.Begin("attachment.write")
				m.Sent(10)
				m.Received(5)
				m.Snapshot()
				end(nil)
			}
		})
	}
	wg.Wait()
	got := m.Snapshot()
	if got["sentBytes"] != int64(8000) || got["receivedBytes"] != int64(4000) || got["pendingRequests"] != 0 {
		t.Fatal(got)
	}
}
