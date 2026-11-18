package hubui

import (
	"fmt"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"strings"
	"testing"
)

func TestLiveDeltaDoesNotMutateEarlierHistoryOrCachedSnapshot(t *testing.T) {
	source := Object{"turns": []Object{
		{"id": "history", "status": "completed", "items": []Object{{"id": "old", "role": "assistant", "text": "已缓存历史"}}},
		{"id": "live", "status": "inProgress", "items": []Object{{"id": "answer", "role": "assistant", "text": "正在"}}},
	}}
	got, refresh := applyEvents(source, []Object{
		{"kind": "text", "turnId": "live", "itemId": "answer", "offset": 2, "delta": "回复"},
		{"kind": "item", "turnId": "live", "itemId": "new", "item": Object{"id": "new", "role": "assistant", "text": "下一条"}},
		{"kind": "turn", "turnId": "live", "status": "completed"},
	})
	oldTurn := objects(source["turns"])[1]
	if str(oldTurn, "status") != "inProgress" || len(objects(oldTurn["items"])) != 1 || str(objects(oldTurn["items"])[0], "text") != "正在" {
		t.Fatal("live event modified a cached snapshot", source)
	}
	rows := messages(got)
	if !refresh || len(rows) != 3 || rows[0].Text != "已缓存历史" || rows[1].Text != "正在回复" || rows[2].Text != "下一条" {
		t.Fatal("copy-on-write lost event ordering or history", got)
	}
}

func BenchmarkApplyLiveDeltaLargeHistory(b *testing.B) {
	turns := make([]Object, 3000)
	for i := range turns {
		turns[i] = Object{"id": fmt.Sprint(i), "status": "completed", "items": []Object{{"id": "answer", "role": "assistant", "text": strings.Repeat("历史消息", 512)}}}
	}
	turns = append(turns, Object{"id": "live", "status": "inProgress", "items": []Object{}})
	source := Object{"turns": turns}
	events := []Object{{"kind": "text", "turnId": "live", "itemId": "answer", "offset": 0, "delta": "实时回复"}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		applyEvents(source, events)
	}
}

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

func TestDirectoryRoutingAndCreationOrder(t *testing.T) {
	rows := []Object{{"id": "same", "providerId": "codex-cli", "sourceKind": "app", "cwd": "/a/project", "updatedAt": 30, "createdAt": 30}, {"id": "same", "providerId": "codex-app", "sourceKind": "app", "cwd": "/a/project", "updatedAt": 29, "createdAt": 29}, {"id": "c", "providerId": "claude-code", "cwd": "/a/project", "updatedAt": 40, "createdAt": 40}, {"id": "other", "providerId": "codex-cli", "sourceKind": "cli", "cwd": "/b/project", "updatedAt": 20, "createdAt": 20}, {"id": "vscode", "providerId": "codex-vscode", "cwd": "/a/project", "updatedAt": 100, "createdAt": 100}}
	d := projectGroups(rows)
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
func TestCompletedTurnFoldsProcessAndKeepsAnswer(t *testing.T) {
	turn := Object{"id": "t", "status": "inProgress", "items": []Object{{"id": "u", "role": "user", "text": "hello"}, {"id": "r", "role": "reasoning", "text": "private reasoning"}, {"id": "a", "role": "assistant", "phase": "commentary", "text": "处理中"}, {"id": "x", "role": "tool", "text": "raw tool output"}, {"id": "f", "role": "assistant", "text": "完成"}}}
	snapshot := Object{"turns": []Object{turn}}
	live := messages(snapshot)
	if len(live) != 4 || live[2].Role != "activity" || !live[3].Live {
		t.Fatalf("bad live projection: %+v", live)
	}
	turn["status"] = "completed"
	done := messages(snapshot)
	if len(done) != 3 || done[1].Role != "work" || len(done[1].Details) != 2 || done[1].Details[0].Text != "处理中" || done[1].Details[1].Role != "activity" || done[2].Text != "完成" || done[1].Live || done[2].Live {
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

func TestStaleSnapshotsDoNotEraseStreamedTail(t *testing.T) {
	old := Object{"turns": []Object{{"id": "history", "status": "completed"}, {"id": "live", "status": "inProgress", "items": []Object{{"id": "answer", "role": "assistant", "text": "已经收到完整的前半段🙂"}, {"id": "tool", "role": "tool", "status": "completed"}}}}}
	stale := Object{"turns": []Object{{"id": "live", "status": "inProgress", "items": []Object{{"id": "answer", "role": "assistant", "text": "已经收到"}}}}}
	merged := mergeLatest(old, stale)
	live := activeTurn(merged)
	if str(objects(live["items"])[0], "text") != "已经收到完整的前半段🙂" || len(objects(live["items"])) != 2 {
		t.Fatal("stale snapshot erased live items")
	}
	if str(objects(objects(stale["turns"])[0]["items"])[0], "text") != "已经收到" {
		t.Fatal("merge mutated the source snapshot")
	}
	for _, snapshot := range []Object{{"turns": []Object{}}, {"turns": []Object{{"id": "history", "status": "completed"}}}} {
		if str(activeTurn(mergeLatest(old, snapshot)), "id") != "live" {
			t.Fatal("offline cache erased a newer streamed turn")
		}
	}
	final := Object{"turns": []Object{{"id": "live", "status": "completed", "items": []Object{{"id": "answer", "role": "assistant", "text": "最终更正"}}}}}
	done := mergeLatest(merged, final)
	if activeTurn(done) != nil || str(objects(objects(done["turns"])[len(objects(done["turns"]))-1]["items"])[0], "text") != "最终更正" {
		t.Fatal("final authoritative snapshot was not applied")
	}
}

func TestImageMarkersAreNotUserMessageText(t *testing.T) {
	m := Message{Role: "user", Text: "看看图片\n[localImage]\n[localImage]\n", Attachments: []Object{{"kind": "image"}, {"kind": "image"}}}
	if got := displayText(m); got != "看看图片" {
		t.Fatal(got)
	}
	if got := messageCopyText(m); got != "看看图片" {
		t.Fatal(got)
	}
	m.Attachments = nil
	if got := displayText(m); got != m.Text {
		t.Fatal("removed literal text without attachment")
	}
	m.Attachments = []Object{{"kind": "image"}}
	m.Text = "解释 [localImage] 的意思，看看这张图。"
	if got := displayText(m); got != m.Text {
		t.Fatal("removed inline literal text")
	}
}

func TestProjectGroupingCombinesWorktreesAndKeepsExecutionDirectories(t *testing.T) {
	project := Object{"id": "gopeed", "name": "Gopeed", "root": "/projects/gopeed"}
	sessions := []Object{
		{"id": "main", "providerId": "codex-app", "cwd": "/projects/gopeed", "projectDir": "/projects/gopeed", "project": project, "updatedAt": 10, "createdAt": 10},
		{"id": "work", "providerId": "codex-cli", "cwd": "/worktrees/feature", "projectDir": "/projects/gopeed", "project": project, "updatedAt": 40, "createdAt": 40},
		{"id": "other", "providerId": "codex-app", "cwd": "/other/gopeed", "projectDir": "/other/gopeed", "project": Object{"id": "other", "name": "Gopeed", "root": "/other/gopeed"}, "updatedAt": 30, "createdAt": 30},
		{"id": "plain", "providerId": "claude-code", "cwd": "/plain/project", "updatedAt": 20, "createdAt": 20},
	}
	groups := projectGroups(sessions)
	if len(groups) != 3 || groups[0].Title != "Gopeed" || groups[0].Path != "/projects/gopeed" || len(groups[0].Sessions) != 2 {
		t.Fatalf("incorrect project grouping: %+v", groups)
	}
	if str(groups[0].Sessions[0], "cwd") != "/worktrees/feature" {
		t.Fatal("changed a session's execution directory")
	}
	var state *State
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		state = newState(cx, newDemoAPI())
		state.sessions = sessions
		state.target = "device"
		return core.View{}
	})
	rows := state.directoryRows()
	found := false
	for _, row := range rows {
		if str(row.Session, "id") == "work" {
			found = true
			if row.Path != "/worktrees/feature" {
				t.Fatal("grouping changed execution path")
			}
		}
	}
	if !found {
		t.Fatal("worktree session missing")
	}
}

func TestMissingProjectDirectoryFallsBackToExecutionDirectory(t *testing.T) {
	sessions := []Object{
		{"id": "a", "providerId": "codex-app", "cwd": "/work/a", "project": Object{"id": "p", "name": "Project"}, "updatedAt": 10, "createdAt": 10},
		{"id": "b", "providerId": "codex-cli", "cwd": "/work/b", "project": Object{"id": "p", "name": "Project"}, "updatedAt": 20, "createdAt": 20},
	}
	groups := projectGroups(sessions)
	if len(groups) != 2 || groups[0].Path != "/work/b" || groups[1].Path != "/work/a" {
		t.Fatalf("native project ID overrode directory grouping: %+v", groups)
	}
}

func TestDirectoryGroupingIsIndependentOfAgentAndNativeProjectID(t *testing.T) {
	sessions := []Object{
		{"id": "same", "providerId": "codex-app", "cwd": "/worktrees/one", "projectDir": "/repo/gopeed", "project": Object{"id": "codex-project", "name": "Gopeed"}, "updatedAt": 10, "createdAt": 10},
		{"id": "same", "providerId": "claude-code", "cwd": "/repo/gopeed", "projectDir": "/repo/gopeed", "project": Object{"id": "claude-project"}, "updatedAt": 20, "createdAt": 20},
		{"id": "same", "providerId": "pi", "cwd": "/worktrees/two", "projectDir": "/repo/gopeed", "updatedAt": 30, "createdAt": 30},
		{"id": "new", "providerId": "future-acp", "cwd": "/repo/gopeed/src", "projectDir": "/repo/gopeed", "updatedAt": 40, "createdAt": 40},
		{"id": "independent", "providerId": "codex-app", "cwd": "/worktrees/standalone", "projectDir": "/worktrees/standalone", "project": Object{"id": "new-project", "name": "Gopeed"}, "updatedAt": 5, "createdAt": 5},
		{"id": "other", "providerId": "pi", "cwd": "/other/gopeed", "projectDir": "/other/gopeed", "updatedAt": 4, "createdAt": 4},
	}
	groups := projectGroups(sessions)
	if len(groups) != 3 || groups[0].Path != "/repo/gopeed" || groups[0].Title != "Gopeed" || len(groups[0].Sessions) != 4 {
		t.Fatalf("Agents did not share the host's project directory: %+v", groups)
	}
	if str(groups[0].Sessions[1], "cwd") != "/worktrees/two" || str(groups[0].Sessions[3], "providerId") != "codex-app" {
		t.Fatal("aggregation changed execution directory or provider route", groups[0])
	}
	// Input order must not change the title or session order.
	for i, j := 0, len(sessions)-1; i < j; i, j = i+1, j-1 {
		sessions[i], sessions[j] = sessions[j], sessions[i]
	}
	again := projectGroups(sessions)
	if again[0].Title != groups[0].Title || str(again[0].Sessions[0], "providerId") != "future-acp" {
		t.Fatal("aggregation depends on input order", again)
	}
}

func TestDifferentNativeProjectsAtSameDirectoryShareOneGroup(t *testing.T) {
	groups := projectGroups([]Object{
		{"id": "a", "providerId": "codex-app", "cwd": "/repo", "projectDir": "/repo", "project": Object{"id": "a", "name": "Work"}},
		{"id": "b", "providerId": "codex-app", "cwd": "/repo", "projectDir": "/repo", "project": Object{"id": "b", "name": "Review"}},
	})
	if len(groups) != 1 || len(groups[0].Sessions) != 2 {
		t.Fatal("native project IDs split one directory")
	}
}

func TestHostWindowsPathsGroupOnAnyController(t *testing.T) {
	groups := projectGroups([]Object{
		{"id": "a", "providerId": "codex-app", "cwd": `C:\Gopeed`, "projectDir": `C:\Gopeed\`},
		{"id": "b", "providerId": "pi", "cwd": `C:\worktree`, "projectDir": "c:/gopeed"},
		{"id": "c", "providerId": "pi", "cwd": "/other", "projectDir": "c:/gopeed-other"},
	})
	if len(groups) != 2 {
		t.Fatal("Windows host paths were interpreted using controller OS", groups)
	}
}
