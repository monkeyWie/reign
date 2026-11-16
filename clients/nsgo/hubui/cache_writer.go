package hubui

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type snapshotWrite struct {
	target, provider, session string
	transcript                Object
}

// A single writer preserves ordering. Coalesce repeated streaming updates so
// IndexedDB/SQLite writes never block painting or grow an unbounded queue.
type snapshotWriter struct {
	mu      sync.Mutex
	pending map[string]snapshotWrite
	wake    chan struct{}
}

func newSnapshotWriter(ctx context.Context, api API) *snapshotWriter {
	w := &snapshotWriter{pending: map[string]snapshotWrite{}, wake: make(chan struct{}, 1)}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
			}
			w.mu.Lock()
			pending := w.pending
			w.pending = map[string]snapshotWrite{}
			w.mu.Unlock()
			for _, item := range pending {
				if ctx.Err() != nil {
					return
				}
				var transcript any
				data, err := json.Marshal(item.transcript)
				if err == nil && json.Unmarshal(data, &transcript) == nil {
					_, _ = api.Call(ctx, "client.snapshot", Object{"provider": item.provider, "session": item.session, "transcript": transcript}, item.target, nil)
				}
			}
		}
	}()
	return w
}

func (s *State) persistSnapshot() {
	if s.snapshots == nil || s.selected == nil || s.transcript == nil {
		return
	}
	s.writeSnapshot(s.target, str(s.selected, "providerId"), str(s.selected, "id"), s.snapshotProjection())
}

func (s *State) writeSnapshot(target, provider, session string, transcript Object) {
	if s.snapshots == nil {
		return
	}
	transcript = copyObject(transcript)
	if pending, ok := transcript["localMessages"].([]*outgoingMessage); ok {
		transcript["localMessages"] = snapshotOutgoing(pending)
	}
	w := s.snapshots
	w.mu.Lock()
	w.pending[target+"|"+provider+"|"+session] = snapshotWrite{target, provider, session, transcript}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (s *State) scheduleSnapshot() {
	if s.snapshotScheduled {
		return
	}
	s.snapshotScheduled = true
	time.AfterFunc(500*time.Millisecond, func() {
		s.cx.Post(func() {
			s.snapshotScheduled = false
			if s.ctx.Err() == nil {
				s.persistSnapshot()
			}
		})
	})
}
