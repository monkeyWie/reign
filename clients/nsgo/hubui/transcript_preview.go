package hubui

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/provider"
)

// A latest-only mailbox with one UI delivery in flight. Slow rendering applies
// backpressure to previews, not to the history download. The trailing update is
// delivered even if the server pauses before returning its final response.
func observeTranscriptPreviews(ctx context.Context, post func(func()), apply func(Object)) func(provider.Transcript) error {
	var mu sync.Mutex
	var history transcriptPreview
	dirty := false
	wake := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
			mu.Lock()
			if !dirty {
				mu.Unlock()
				continue
			}
			partial := history.snapshot()
			dirty = false
			mu.Unlock()
			applied := make(chan struct{})
			post(func() {
				defer close(applied)
				if ctx.Err() == nil {
					apply(partial)
				}
			})
			select {
			case <-ctx.Done():
				return
			case <-applied:
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return func(fragment provider.Transcript) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		err := history.add(fragment)
		dirty = true
		mu.Unlock()
		if err != nil {
			return err
		}
		select {
		case wake <- struct{}{}:
		default:
		}
		return ctx.Err()
	}
}

// Convert each wire fragment once, rather than JSON-copying the entire growing
// history on every preview. Published objects remain immutable for the UI/cache.
type transcriptPreview struct {
	metadata Object
	turns    []Object
	indices  map[string]int
	items    map[string]map[string]int
	editable map[string]bool
	texts    map[string]*provider.TranscriptTextBuffer
}

func (b *transcriptPreview) add(fragment provider.Transcript) error {
	raw, err := json.Marshal(fragment)
	if err != nil {
		return err
	}
	value := Object{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	b.metadata = value
	if b.indices == nil {
		b.indices, b.items = map[string]int{}, map[string]map[string]int{}
		b.texts = map[string]*provider.TranscriptTextBuffer{}
	}
	if b.editable == nil {
		b.editable = map[string]bool{}
	}
	for _, turn := range objects(value["turns"]) {
		id := str(turn, "id")
		i, exists := b.indices[id]
		if !exists {
			i = len(b.turns)
			b.indices[id], b.items[id] = i, map[string]int{}
			b.turns = append(b.turns, Object{"id": id, "items": []Object{}})
		}
		if !b.editable[id] {
			b.turns[i] = copyObject(b.turns[i])
			b.turns[i]["items"] = append([]Object(nil), objects(b.turns[i]["items"])...)
			b.editable[id] = true
		}
		target := b.turns[i]
		target["status"] = turn["status"]
		items := objects(target["items"])
		for _, item := range objects(turn["items"]) {
			key := str(item, "id")
			textKey := id + "\x00" + key
			if _, chunked := item["textOffset"]; chunked {
				textBuffer := b.texts[textKey]
				if textBuffer == nil {
					textBuffer = &provider.TranscriptTextBuffer{}
					b.texts[textKey] = textBuffer
				}
				text, err := textBuffer.Append(str(item, "text"), int(number(item, "textOffset")))
				if err != nil {
					return err
				}
				item["text"] = text
				delete(item, "textOffset")
			} else {
				delete(b.texts, textKey)
			}
			if j, exists := b.items[id][key]; exists {
				items[j] = item
			} else {
				b.items[id][key] = len(items)
				items = append(items, item)
			}
		}
		target["items"] = items
	}
	return nil
}

func (b *transcriptPreview) snapshot() Object {
	value := copyObject(b.metadata)
	value["turns"] = append([]Object(nil), b.turns...)
	b.editable = nil
	return value
}
