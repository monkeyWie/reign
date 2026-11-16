package transport

import (
	"sync"
	"time"
)

const ReadIdleTimeout = 30 * time.Second

// ReadIdle measures silence of this RPC, not socket liveness or total transfer
// time. Other chats' traffic cannot keep a stalled read pending indefinitely.
type ReadIdle struct {
	mu    sync.Mutex
	last  time.Time
	timer *time.Timer
	C     <-chan time.Time
}

func NewReadIdle(enabled bool) *ReadIdle {
	i := &ReadIdle{}
	if enabled {
		i.last = time.Now()
		i.timer = time.NewTimer(ReadIdleTimeout)
		i.C = i.timer.C
	}
	return i
}
func (i *ReadIdle) Touch() {
	if i != nil && i.timer != nil {
		i.mu.Lock()
		i.last = time.Now()
		i.mu.Unlock()
	}
}
func (i *ReadIdle) Expired() bool {
	i.mu.Lock()
	last := i.last
	i.mu.Unlock()
	remaining := time.Until(last.Add(ReadIdleTimeout))
	if remaining > 0 {
		i.timer.Reset(remaining)
		return false
	}
	return true
}
func (i *ReadIdle) Stop() {
	if i.timer != nil {
		i.timer.Stop()
	}
}
