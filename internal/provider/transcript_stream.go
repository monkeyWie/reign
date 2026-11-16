package provider

import "context"

type transcriptObserverKey struct{}

// WithTranscriptObserver delivers immutable history fragments in source order.
// The final Transcript is still authoritative; fragments may be interrupted.
func WithTranscriptObserver(ctx context.Context, observe func(Transcript) error) context.Context {
	return context.WithValue(ctx, transcriptObserverKey{}, observe)
}

func ObserveTranscript(ctx context.Context, fragment Transcript) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if observe, _ := ctx.Value(transcriptObserverKey{}).(func(Transcript) error); observe != nil {
		return observe(fragment)
	}
	return nil
}

// EmitTranscriptTurn sends each message separately, retaining empty turn status.
func EmitTranscriptTurn(ctx context.Context, metadata Transcript, turn Turn) error {
	metadata.Turns = []Turn{{ID: turn.ID, Status: turn.Status, Items: []Item{}}}
	if len(turn.Items) == 0 {
		return ObserveTranscript(ctx, metadata)
	}
	for _, item := range turn.Items {
		metadata.Turns = []Turn{{ID: turn.ID, Status: turn.Status, Items: []Item{item}}}
		if err := ObserveTranscript(ctx, metadata); err != nil {
			return err
		}
	}
	return nil
}

// TranscriptBuffer assembles one snapshot, without treating a partial turn as
// deletion of its other messages. Callers publish copies, never these slices.
type TranscriptBuffer struct {
	Transcript
	Seen  bool
	turns map[string]int
	items map[string]map[string]int
}

func (b *TranscriptBuffer) Add(fragment Transcript) {
	b.Seen = true
	b.StreamEpoch, b.StreamCursor = fragment.StreamEpoch, fragment.StreamCursor
	b.Session, b.NextCursor = fragment.Session, fragment.NextCursor
	if b.turns == nil {
		b.turns, b.items = map[string]int{}, map[string]map[string]int{}
		b.Turns = []Turn{}
	}
	for _, turn := range fragment.Turns {
		index, found := b.turns[turn.ID]
		if !found {
			index = len(b.Turns)
			b.turns[turn.ID] = index
			b.items[turn.ID] = map[string]int{}
			b.Turns = append(b.Turns, Turn{ID: turn.ID, Items: []Item{}})
		}
		target := &b.Turns[index]
		target.Status = turn.Status
		for _, item := range turn.Items {
			j, exists := b.items[turn.ID][item.ID]
			if exists {
				target.Items[j] = item
			} else {
				b.items[turn.ID][item.ID] = len(target.Items)
				target.Items = append(target.Items, item)
			}
		}
	}
}
