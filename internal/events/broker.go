package events

import (
	"encoding/json"
	"sync"
	"time"
)

type Event struct {
	ID         uint64          `json:"id"`
	ProviderID string          `json:"providerId,omitempty"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	Time       time.Time       `json:"time"`
}

type Broker struct {
	mu          sync.Mutex
	next        uint64
	history     []Event
	subscribers map[chan Event]struct{}
}

func New() *Broker { return &Broker{subscribers: make(map[chan Event]struct{})} }

func (b *Broker) Publish(providerID, kind string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	event := Event{ID: b.next, ProviderID: providerID, Type: kind, Data: raw, Time: time.Now()}
	b.history = append(b.history, event)
	if len(b.history) > 256 {
		b.history = b.history[len(b.history)-256:]
	}
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Slow clients reconnect and fetch a fresh snapshot; never block an agent.
			close(ch)
			delete(b.subscribers, ch)
		}
	}
}

func (b *Broker) Subscribe(after uint64) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 300)
	if after > 0 {
		if len(b.history) > 0 && after+1 < b.history[0].ID {
			ch <- Event{ID: b.next, Type: "resync", Data: json.RawMessage(`{}`), Time: time.Now()}
		} else {
			for _, e := range b.history {
				if e.ID > after {
					ch <- e
				}
			}
		}
	}
	b.subscribers[ch] = struct{}{}
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if _, ok := b.subscribers[ch]; ok {
				delete(b.subscribers, ch)
				close(ch)
			}
		})
	}
}
