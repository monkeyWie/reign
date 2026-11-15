package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// StreamEvent contains only presentation data, never raw protocol notifications.
// Text offsets use UTF-16 code units, matching Flutter and browser strings.
type StreamEvent struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	TurnID    string `json:"turnId,omitempty"`
	ItemID    string `json:"itemId,omitempty"`
	Kind      string `json:"kind"`
	Status    string `json:"status,omitempty"`
	Item      *Item  `json:"item,omitempty"`
	Delta     string `json:"delta,omitempty"`
	Offset    int    `json:"offset,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type StreamBatch struct {
	Epoch     string        `json:"epoch"`
	Cursor    int64         `json:"cursor"`
	Reset     bool          `json:"reset,omitempty"`
	Heartbeat bool          `json:"heartbeat,omitempty"`
	Events    []StreamEvent `json:"events"`
}

type StreamProber interface {
	Probe(context.Context, string, string, int64) (StreamBatch, error)
}

type StreamingProvider interface {
	Watch(context.Context, string, string, int64) (StreamBatch, error)
}

// EventLog is a bounded replay buffer. Readers that fall behind fetch a fresh
// transcript; slow readers never block the Agent's notification loop.
type EventLog struct {
	mu      sync.Mutex
	epoch   string
	next    int64
	bytes   int
	history []StreamEvent
	sizes   []int
	changed chan struct{}
}

func NewEventLog() *EventLog {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err)
	}
	return &EventLog{epoch: hex.EncodeToString(nonce[:]), changed: make(chan struct{})}
}

// Reset invalidates cursors after the native Agent connection is replaced.
func (l *EventLog) Reset() {
	fresh := NewEventLog()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.epoch, l.next, l.bytes = fresh.epoch, 0, 0
	l.history, l.sizes = nil, nil
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *EventLog) Publish(event StreamEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	event.ID = l.next
	raw, _ := json.Marshal(event)
	l.history = append(l.history, event)
	l.sizes = append(l.sizes, len(raw))
	l.bytes += len(raw)
	for len(l.history) > 1024 || l.bytes > 2<<20 {
		l.bytes -= l.sizes[0]
		l.history[0] = StreamEvent{}
		l.history = l.history[1:]
		l.sizes = l.sizes[1:]
	}
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *EventLog) Watch(ctx context.Context, session, epoch string, after int64) (StreamBatch, error) {
	return l.read(ctx, session, epoch, after, false)
}

// Probe returns a replay boundary immediately, even when already caught up.
func (l *EventLog) Probe(ctx context.Context, session, epoch string, after int64) (StreamBatch, error) {
	return l.read(ctx, session, epoch, after, true)
}

func (l *EventLog) Position() (string, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.epoch, l.next
}

func (l *EventLog) read(ctx context.Context, session, epoch string, after int64, probe bool) (StreamBatch, error) {
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	for {
		l.mu.Lock()
		batch := StreamBatch{Epoch: l.epoch, Cursor: l.next, Events: []StreamEvent{}}
		oldest := l.next + 1
		if len(l.history) > 0 {
			oldest = l.history[0].ID
		}
		if epoch != l.epoch || after < oldest-1 || after > l.next {
			batch.Reset = true
			l.mu.Unlock()
			return batch, nil
		}
		if after < l.next || probe {
			for _, event := range l.history {
				if event.ID > after && event.SessionID == session {
					batch.Events = append(batch.Events, event)
				}
			}
			l.mu.Unlock()
			return batch, nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-changed:
		case <-timeout.C:
			batch.Heartbeat = true
			return batch, nil
		case <-ctx.Done():
			return StreamBatch{}, ctx.Err()
		}
	}
}
