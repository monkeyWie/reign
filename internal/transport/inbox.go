package transport

import (
	"errors"
	"sync"
)

type queuedResponse[T any] struct {
	value T
	size  int
}

// Inbox keeps slow history consumers off the shared socket reader. Responses
// stay ordered per RPC, with a strict memory bound instead of an unbounded
// goroutine per notification or backpressure across unrelated calls.
type Inbox[T any] struct {
	mu          sync.Mutex
	Ready       chan struct{}
	queue       []queuedResponse[T]
	head, bytes int
	err         error
	closed      bool
}

func NewInbox[T any]() *Inbox[T] { return &Inbox[T]{Ready: make(chan struct{}, 1)} }
func (i *Inbox[T]) wake() {
	select {
	case i.Ready <- struct{}{}:
	default:
	}
}
func (i *Inbox[T]) Push(v T, size int) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.err != nil {
		return
	}
	if size < 0 || i.bytes+size > MaxPayload || len(i.queue)-i.head >= 8192 {
		i.err = errors.New("设备响应积压过多，已取消本次读取，请重试")
		i.queue = nil
		i.head = 0
		i.bytes = 0
		i.wake()
		return
	}
	if i.head > 0 && i.head >= len(i.queue)/2 {
		i.queue = append([]queuedResponse[T](nil), i.queue[i.head:]...)
		i.head = 0
	}
	i.queue = append(i.queue, queuedResponse[T]{v, size})
	i.bytes += size
	i.wake()
}
func (i *Inbox[T]) Take() (value T, ok bool, err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.err != nil {
		return value, false, i.err
	}
	if i.head >= len(i.queue) {
		return value, false, nil
	}
	entry := i.queue[i.head]
	i.queue[i.head] = queuedResponse[T]{}
	i.head++
	i.bytes -= entry.size
	if i.head < len(i.queue) {
		i.wake()
	} else {
		i.queue = nil
		i.head = 0
	}
	return entry.value, true, nil
}
func (i *Inbox[T]) Close() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	i.queue = nil
	i.head = 0
	i.bytes = 0
}
