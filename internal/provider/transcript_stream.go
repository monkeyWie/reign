package provider

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const TranscriptTextChunkSize = 2048

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
		if !utf8.ValidString(item.Text) {
			item.Text = strings.ToValidUTF8(item.Text, "\uFFFD")
		}
		for offset := 0; ; {
			part := item
			if len(item.Text) > TranscriptTextChunkSize {
				end := min(offset+TranscriptTextChunkSize, len(item.Text))
				for end < len(item.Text) && !utf8.RuneStart(item.Text[end]) {
					end--
				}
				part.Text, part.TextOffset = item.Text[offset:end], new(offset)
				offset = end
			}
			metadata.Turns = []Turn{{ID: turn.ID, Status: turn.Status, Items: []Item{part}}}
			if err := ObserveTranscript(ctx, metadata); err != nil {
				return err
			}
			if part.TextOffset == nil || offset == len(item.Text) {
				break
			}
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
	texts map[string]map[string]*TranscriptTextBuffer
}

func (b *TranscriptBuffer) Add(fragment Transcript) error {
	b.Seen = true
	b.StreamEpoch, b.StreamCursor = fragment.StreamEpoch, fragment.StreamCursor
	b.Session, b.NextCursor = fragment.Session, fragment.NextCursor
	if b.turns == nil {
		b.turns, b.items, b.texts = map[string]int{}, map[string]map[string]int{}, map[string]map[string]*TranscriptTextBuffer{}
		b.Turns = []Turn{}
	}
	for _, turn := range fragment.Turns {
		index, found := b.turns[turn.ID]
		if !found {
			index = len(b.Turns)
			b.turns[turn.ID] = index
			b.items[turn.ID] = map[string]int{}
			b.texts[turn.ID] = map[string]*TranscriptTextBuffer{}
			b.Turns = append(b.Turns, Turn{ID: turn.ID, Items: []Item{}})
		}
		target := &b.Turns[index]
		target.Status = turn.Status
		for _, item := range turn.Items {
			j, exists := b.items[turn.ID][item.ID]
			if item.TextOffset != nil {
				textBuffer := b.texts[turn.ID][item.ID]
				if textBuffer == nil {
					textBuffer = &TranscriptTextBuffer{}
					b.texts[turn.ID][item.ID] = textBuffer
				}
				text, err := textBuffer.Append(item.Text, *item.TextOffset)
				if err != nil {
					return err
				}
				item.Text, item.TextOffset = text, nil
			} else {
				delete(b.texts[turn.ID], item.ID)
			}
			if exists {
				target.Items[j] = item
			} else {
				b.items[turn.ID][item.ID] = len(target.Items)
				target.Items = append(target.Items, item)
			}
		}
	}
	return nil
}

// TranscriptTextBuffer accepts duplicate/overlapping parts, never a missing
// prefix or conflicting bytes. Offsets are bytes, and fragments end at UTF-8
// boundaries so every intermediate message remains valid renderable text.
// Builder.String publishes immutable prefixes without copying the growing
// message on every network fragment (quadratic work for a large reply).
type TranscriptTextBuffer struct{ text strings.Builder }

func (b *TranscriptTextBuffer) Append(delta string, offset int) (string, error) {
	previous := b.text.String()
	if offset < 0 || offset > len(previous) || !utf8.ValidString(delta) || offset < len(previous) && !utf8.RuneStart(previous[offset]) {
		return "", errors.New("消息文本分片不连续，请重新同步")
	}
	overlap := min(len(previous)-offset, len(delta))
	if previous[offset:offset+overlap] != delta[:overlap] {
		return "", errors.New("消息文本分片内容冲突，请重新同步")
	}
	b.text.WriteString(delta[overlap:])
	return b.text.String(), nil
}
