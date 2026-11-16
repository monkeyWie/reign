package transport

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
	"time"
)

func TestCompressionReducesHistoryAndBoundsDecoding(t *testing.T) {
	history := bytes.Repeat([]byte(`{"id":"message","text":"中文消息 and repeated tool output"}`), 20000)
	packed, encoding, err := Encode(history)
	if err != nil || encoding != Gzip || len(packed) > len(history)/10 {
		t.Fatalf("history not reduced sufficiently: %d -> %d, %s %v", len(history), len(packed), encoding, err)
	}
	decoded, err := Decode(packed, encoding, len(history))
	if err != nil || !bytes.Equal(decoded, history) {
		t.Fatal("history roundtrip failed", err)
	}
	if _, err := Decode(packed, encoding, 100); err == nil {
		t.Fatal("decompression exceeded declared size")
	}
	if _, err := Decode(packed, encoding, MaxPayload+1); err == nil {
		t.Fatal("unbounded decompression accepted")
	}
	if _, err := Decode(packed[:len(packed)-4], encoding, len(history)); err == nil {
		t.Fatal("truncated gzip accepted")
	}
	t.Logf("text history: %d -> %d bytes (%.1f%% reduction)", len(history), len(packed), 100*(1-float64(len(packed))/float64(len(history))))
}
func TestIncompressibleAttachmentsAndTinyMessagesKeepTheirBytes(t *testing.T) {
	random := make([]byte, 128<<10)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{random, []byte("new user message")} {
		packed, encoding, err := Encode(raw)
		if err != nil || encoding != Identity || !bytes.Equal(packed, raw) {
			t.Fatal("unnecessary compression", encoding, err)
		}
	}
}
func awaitQueue(t *testing.T, s *Sender, p Priority, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(s.queues[p]) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("send did not queue")
}
func TestUrgentFramePreemptsBulkAndCancelledWorkDoesNotWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var s Sender
	blocked := make(chan struct{})
	started := make(chan struct{})
	order := make(chan string, 16)
	done := make(chan error, 16)
	go func() {
		done <- s.Send(ctx, ctx, Bulk, func() error { close(started); <-blocked; order <- "first"; return nil })
	}()
	<-started
	bulkCtx, stopBulk := context.WithCancel(ctx)
	for range 4 {
		go func() { done <- s.Send(bulkCtx, ctx, Bulk, func() error { order <- "bulk"; return nil }) }()
	}
	awaitQueue(t, &s, Bulk, 4)
	go func() { done <- s.Send(ctx, ctx, Urgent, func() error { order <- "live"; return nil }) }()
	awaitQueue(t, &s, Urgent, 1)
	stopBulk()
	close(blocked)
	if first, second := <-order, <-order; first != "first" || second != "live" {
		t.Fatal("live response remained behind history", first, second)
	}
	for range 6 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("sender leaked")
		}
	}
	select {
	case v := <-order:
		t.Fatal("cancelled bulk frame wrote", v)
	default:
	}
}
func TestContinuousUrgentTrafficDoesNotStarveHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var s Sender
	blocked := make(chan struct{})
	started := make(chan struct{})
	order := make(chan Priority, 32)
	done := make(chan error, 32)
	go func() { done <- s.Send(ctx, ctx, Urgent, func() error { close(started); <-blocked; return nil }) }()
	<-started
	for range 16 {
		go func() { done <- s.Send(ctx, ctx, Urgent, func() error { order <- Urgent; return nil }) }()
	}
	go func() { done <- s.Send(ctx, ctx, Bulk, func() error { order <- Bulk; return nil }) }()
	awaitQueue(t, &s, Urgent, 16)
	awaitQueue(t, &s, Bulk, 1)
	close(blocked)
	for i := 0; i < 8; i++ {
		select {
		case p := <-order:
			if p == Bulk {
				for range 18 {
					<-done
				}
				return
			}
		case <-time.After(time.Second):
			t.Fatal("sender stalled")
		}
	}
	t.Fatal("urgent traffic starved the history frame")
}

func TestInboxPreservesOrderAndBoundsAccumulatedResponses(t *testing.T) {
	inbox := NewInbox[int]()
	defer inbox.Close()
	for i := range 40 {
		inbox.Push(i, 1024)
	}
	for i := range 40 {
		select {
		case <-inbox.Ready:
		case <-time.After(time.Second):
			t.Fatal("queue lost readiness")
		}
		value, ok, err := inbox.Take()
		if err != nil || !ok || value != i {
			t.Fatal("response order changed", value, ok, err)
		}
	}
	inbox.Push(1, MaxPayload)
	inbox.Push(2, 1)
	if _, _, err := inbox.Take(); err == nil {
		t.Fatal("unbounded receive queue")
	}
}
