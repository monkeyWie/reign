package provider

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHistoryStreamsLongReplyBeforeFollowingUserMessage(t *testing.T) {
	answer := strings.Repeat("长回复🙂 mixed text.\n", 2000)
	turn := Turn{ID: "turn", Status: "completed", Items: []Item{
		{ID: "u1", Role: "user", Text: "first question"},
		{ID: "a1", Role: "assistant", Phase: "commentary", Text: answer},
		{ID: "u2", Role: "user", Text: "next question"},
	}}
	var history TranscriptBuffer
	var firstPrefix string
	parts := 0
	ctx := WithTranscriptObserver(context.Background(), func(fragment Transcript) error {
		item := fragment.Turns[0].Items[0]
		if !utf8.ValidString(item.Text) || len(item.Text) > TranscriptTextChunkSize {
			t.Fatal("history did not deliver renderable bounded text", len(item.Text))
		}
		if err := history.Add(fragment); err != nil {
			return err
		}
		items := history.Turns[0].Items
		if item.ID == "a1" {
			parts++
			if len(items) != 2 || items[1].TextOffset != nil {
				t.Fatal("following message overtook the reply or transport offsets leaked")
			}
			if parts == 1 {
				firstPrefix = items[1].Text
				if firstPrefix == answer || firstPrefix == "" || !strings.HasPrefix(answer, firstPrefix) {
					t.Fatal("reply waited for complete text")
				}
			}
		}
		if item.ID == "u2" && items[1].Text != answer {
			t.Fatal("next question arrived before the complete earlier reply")
		}
		return nil
	})
	if err := EmitTranscriptTurn(ctx, Transcript{}, turn); err != nil {
		t.Fatal(err)
	}
	if parts < 2 || len(history.Turns[0].Items) != 3 || history.Turns[0].Items[1].Text != answer || !strings.HasPrefix(answer, firstPrefix) {
		t.Fatal("history lost text, order, or an immutable published prefix")
	}
}

func TestHistoryTextRejectsGapsAndConflictsAndAcceptsReplay(t *testing.T) {
	var text TranscriptTextBuffer
	first, err := text.Append("你好🙂", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := text.Append("gap", len(first)+1); err == nil {
		t.Fatal("missing prefix accepted")
	}
	if _, err := text.Append("conflict", 0); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	if _, err := text.Append("bad boundary", 1); err == nil {
		t.Fatal("offset split a UTF-8 character")
	}
	if got, err := text.Append("🙂继续", len("你好")); err != nil || got != "你好🙂继续" || first != "你好🙂" {
		t.Fatal("overlap or immutable prefix failed", got, err)
	}
	if got, err := text.Append("你好🙂继续", 0); err != nil || got != "你好🙂继续" {
		t.Fatal("duplicate fragment repeated text", got, err)
	}
}
