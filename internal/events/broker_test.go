package events

import (
	"testing"
)

func TestReplayAndSlowSubscriber(t *testing.T) {
	b := New()
	for i := 0; i < 300; i++ {
		b.Publish("test", "event", i)
	}
	ch, closeSub := b.Subscribe(1)
	if e := <-ch; e.Type != "resync" {
		t.Fatal("missing resync")
	}
	closeSub()
	closeSub()
	ch, closeSub = b.Subscribe(299)
	defer closeSub()
	if e := <-ch; e.ID != 300 {
		t.Fatal("bad cursor")
	}
	for i := 0; i < 302; i++ {
		b.Publish("test", "event", i)
	}
	for range ch {
	}
	b.Publish("test", "still-alive", nil)
}
