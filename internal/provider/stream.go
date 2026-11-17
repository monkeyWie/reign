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
// Text offsets use UTF-16 code units, matching Codex events and browser strings.
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
const (
	replaySessionBytes = 2 << 20
	replayTotalBytes   = 8 << 20
	replaySessionLimit = 128
)

type sessionReplay struct {
	history []StreamEvent
	sizes   []int
	bytes   int
	dropped int64
	touched int64
}
type EventLog struct {
	mu       sync.Mutex
	epoch    string
	next     int64
	bytes    int
	sessions map[string]*sessionReplay
	// An evicted session with an older cursor must fetch a new baseline.
	evictedThrough int64
	changed        chan struct{}
}

func NewEventLog() *EventLog {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err)
	}
	return &EventLog{epoch: hex.EncodeToString(nonce[:]), sessions: map[string]*sessionReplay{}, changed: make(chan struct{})}
}

// Reset invalidates cursors after the native Agent connection is replaced.
func (l *EventLog) Reset() {
	fresh := NewEventLog()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.epoch, l.next, l.bytes = fresh.epoch, 0, 0
	l.sessions = map[string]*sessionReplay{}
	l.evictedThrough = 0
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *EventLog) Publish(event StreamEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	event.ID = l.next
	r := l.sessions[event.SessionID]
	if r == nil {
		if len(l.sessions) >= replaySessionLimit {
			oldest := event.SessionID
			stamp := l.next
			for id, entry := range l.sessions {
				if entry.touched < stamp {
					oldest, stamp = id, entry.touched
				}
			}
			l.bytes -= l.sessions[oldest].bytes
			delete(l.sessions, oldest)
			// Conservative only for evicted entries; retained sessions keep their own watermark.
			l.evictedThrough = l.next
		}
		r = &sessionReplay{dropped: l.evictedThrough}
		l.sessions[event.SessionID] = r
	}
	r.touched = l.next
	raw, _ := json.Marshal(event)
	r.history = append(r.history, event)
	r.sizes = append(r.sizes, len(raw))
	r.bytes += len(raw)
	l.bytes += len(raw)
	trim := func(entry *sessionReplay) {
		size := entry.sizes[0]
		entry.dropped = entry.history[0].ID
		entry.history[0] = StreamEvent{}
		entry.history, entry.sizes = entry.history[1:], entry.sizes[1:]
		entry.bytes -= size
		l.bytes -= size
	}
	for len(r.history) > 1024 || r.bytes > replaySessionBytes {
		trim(r)
	}
	// Reclaim from the largest producer, never simply from the oldest event globally.
	for l.bytes > replayTotalBytes {
		largest := r
		for _, entry := range l.sessions {
			if entry.bytes > largest.bytes {
				largest = entry
			}
		}
		trim(largest)
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
		replay := l.sessions[session]
		dropped := l.evictedThrough
		if replay != nil {
			dropped = replay.dropped
		}
		if epoch != l.epoch || after < dropped || after > l.next {
			batch.Reset = true
			l.mu.Unlock()
			return batch, nil
		}
		if after < l.next || probe {
			if replay != nil {
				for _, event := range replay.history {
					if event.ID > after {
						batch.Events = append(batch.Events, event)
					}
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
