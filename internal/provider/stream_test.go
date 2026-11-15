package provider

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEventLogReplayIsolationAndRecovery(t *testing.T) {
	log := NewEventLog()
	base, err := log.Watch(context.Background(), "a", "", 0)
	if err != nil || !base.Reset || base.Epoch == "" {
		t.Fatal("missing initial snapshot boundary", base, err)
	}
	log.Publish(StreamEvent{SessionID: "b", Kind: "text", Delta: "other session"})
	log.Publish(StreamEvent{SessionID: "a", Kind: "text", Delta: "visible"})
	batch, err := log.Watch(context.Background(), "a", base.Epoch, base.Cursor)
	if err != nil || batch.Reset || len(batch.Events) != 1 || batch.Events[0].Delta != "visible" || batch.Cursor != 2 {
		t.Fatal("bad isolated replay", batch, err)
	}
	for i := 0; i < 1030; i++ {
		log.Publish(StreamEvent{SessionID: "a", Kind: "text", Delta: "x"})
	}
	batch, err = log.Watch(context.Background(), "a", base.Epoch, base.Cursor)
	if err != nil || !batch.Reset || len(batch.Events) != 0 {
		t.Fatal("slow reader must resnapshot", batch, err)
	}
	for i := 0; i < 4; i++ {
		log.Publish(StreamEvent{SessionID: "a", Kind: "text", Delta: strings.Repeat("x", 800000)})
	}
	if log.bytes > 2<<20 {
		t.Fatal("unbounded replay bytes")
	}
	log.Reset()
	reset, err := log.Watch(context.Background(), "a", base.Epoch, batch.Cursor)
	if err != nil || !reset.Reset || reset.Epoch == base.Epoch {
		t.Fatal("replaced Agent connection retained stale cursors")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := log.Watch(ctx, "a", reset.Epoch, log.next); err != context.Canceled {
		t.Fatal("watch must cancel without blocking shutdown", err)
	}
}

func TestProbeReturnsCaughtUpBoundaryWithoutLongPolling(t *testing.T) {
	log := NewEventLog()
	epoch, cursor := log.Position()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	batch, err := log.Probe(ctx, "a", epoch, cursor)
	if err != nil || batch.Reset || batch.Heartbeat || batch.Cursor != cursor || len(batch.Events) != 0 {
		t.Fatal("caught-up probe waited or invalidated history", batch, err)
	}
	log.Publish(StreamEvent{SessionID: "b", Kind: "text", Delta: "private"})
	log.Publish(StreamEvent{SessionID: "a", Kind: "text", Delta: "visible"})
	batch, err = log.Probe(ctx, "a", epoch, cursor)
	if err != nil || batch.Cursor != 2 || len(batch.Events) != 1 || batch.Events[0].Delta != "visible" {
		t.Fatal("probe failed to replay isolated events", batch, err)
	}
	log.Reset()
	batch, err = log.Probe(ctx, "a", epoch, batch.Cursor)
	if err != nil || !batch.Reset || batch.Epoch == epoch {
		t.Fatal("probe accepted stale epoch", batch, err)
	}
}
