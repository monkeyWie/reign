package hubui

import (
	"strings"
	"testing"
)

func TestQuestionReplyPresentationKeepsOriginalMessage(t *testing.T) {
	source := "<send_user_message_question_reply>\n[{\"questionItemId\":\"internal\",\"question\":\"允许吗？\",\"answer\":\"允许\"}]\n</send_user_message_question_reply>"
	m := Message{Role: "user", Text: source}
	for _, text := range []string{messageCopyText(m), messagePreviewText(m)} {
		if text != "问题：允许吗？\n回答：允许" {
			t.Fatalf("unreadable reply: %q", text)
		}
	}
	if m.Text != source || displayText(m) != source {
		t.Fatal("rewrote stored source")
	}
	m.Role = "assistant"
	if messageCopyText(m) != source || messagePreviewText(m) != source {
		t.Fatal("parsed assistant example as user reply")
	}
}

func TestDirectoryRoutingAndRecentOrder(t *testing.T) {
	rows := []Object{{"id": "same", "providerId": "codex-cli", "sourceKind": "app", "cwd": "/a/project", "updatedAt": 30}, {"id": "same", "providerId": "codex-app", "sourceKind": "app", "cwd": "/a/project", "updatedAt": 29}, {"id": "c", "providerId": "claude-code", "cwd": "/a/project", "updatedAt": 40}, {"id": "other", "providerId": "codex-cli", "sourceKind": "cli", "cwd": "/b/project", "updatedAt": 20}, {"id": "vscode", "providerId": "codex-vscode", "cwd": "/a/project", "updatedAt": 100}}
	d := directories(rows)
	if len(d) != 2 || d[0].Path != "/a/project" || len(d[0].Sessions) != 2 || str(d[0].Sessions[0], "id") != "c" || str(d[0].Sessions[1], "providerId") != "codex-app" {
		t.Fatalf("lost route/group/order: %#v", d)
	}
}
func TestStreamingUTF16AndReplay(t *testing.T) {
	source := Object{"turns": []Object{{"id": "t", "status": "inProgress", "items": []Object{{"id": "i", "role": "assistant", "text": "好🙂"}}}}}
	e := Object{"kind": "text", "turnId": "t", "itemId": "i", "offset": 1, "delta": "🙂 hello"}
	got, reset := applyEvents(source, []Object{e, e})
	if reset {
		t.Fatal("valid overlap requested reset")
	}
	text := str(objects(objects(got["turns"])[0]["items"])[0], "text")
	if text != "好🙂 hello" {
		t.Fatalf("text = %q", text)
	}
	if str(objects(objects(source["turns"])[0]["items"])[0], "text") != "好🙂" {
		t.Fatal("mutated snapshot")
	}
	_, reset = applyEvents(got, []Object{{"kind": "text", "turnId": "t", "itemId": "i", "offset": 500, "delta": "gap"}})
	if !reset {
		t.Fatal("gap needs a snapshot")
	}
}
func TestCompletedTurnHidesTransientSteps(t *testing.T) {
	turn := Object{"id": "t", "status": "inProgress", "items": []Object{{"id": "u", "role": "user", "text": "hello"}, {"id": "r", "role": "reasoning", "text": "private reasoning"}, {"id": "a", "role": "assistant", "phase": "commentary", "text": "处理中"}, {"id": "x", "role": "tool", "text": "raw tool output"}, {"id": "f", "role": "assistant", "text": "完成"}}}
	snapshot := Object{"turns": []Object{turn}}
	live := messages(snapshot)
	if len(live) != 4 || live[2].Role != "activity" || !live[3].Live {
		t.Fatalf("bad live projection: %+v", live)
	}
	turn["status"] = "completed"
	done := messages(snapshot)
	if len(done) != 2 || done[1].Text != "完成" || done[1].Live {
		t.Fatalf("bad completed projection: %+v", done)
	}
}
func TestReconnectGapDoesNotInventHistory(t *testing.T) {
	old := Object{"turns": []Object{{"id": "a"}, {"id": "b"}}, "nextCursor": "old"}
	latest := Object{"turns": []Object{{"id": "z"}}, "nextCursor": "new"}
	got := mergeLatest(old, latest)
	if len(objects(got["turns"])) != 1 || str(got, "nextCursor") != "new" {
		t.Fatal("stitched disconnected history across missing turns")
	}
	got = mergeLatest(old, Object{"turns": []Object{{"id": "b"}, {"id": "c"}}, "nextCursor": "new"})
	if len(objects(got["turns"])) != 3 || str(got, "nextCursor") != "old" {
		t.Fatal("lost contiguous cached history")
	}
}
func TestOnlyHideResolvedAttachmentMetadata(t *testing.T) {
	body := "# Files mentioned by the user:\n\n## photo.png: /tmp/photo.png\nImage attachment: true\n\n## My request:\n看看图片"
	m := Message{Role: "user", Text: body, Attachments: []Object{{"source": "/tmp/photo.png"}}}
	if displayText(m) != "看看图片" {
		t.Fatal(displayText(m))
	}
	m.Attachments = []Object{{"source": "/other"}}
	if !strings.HasPrefix(displayText(m), "# Files") {
		t.Fatal("unresolved text was removed")
	}
}

func TestSupersededTurnDoesNotRemainActive(t *testing.T) {
	if activeTurn(Object{"turns": []Object{{"id": "old", "status": "inProgress"}, {"id": "new", "status": "completed"}}}) != nil {
		t.Fatal("superseded turn remained active")
	}
}
