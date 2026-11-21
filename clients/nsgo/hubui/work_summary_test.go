package hubui

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func workSummaryTranscript(status string, final bool) Object {
	items := []Object{
		{"id": "u", "role": "user", "text": "调整附件下载交互"},
		{"id": "progress", "role": "assistant", "phase": "commentary", "text": "正在调整附件下载交互。"},
		{"id": "tool", "role": "tool", "status": "completed", "text": "RAW_OUTPUT_SENTINEL", "activities": []Object{{"kind": "edit", "title": "修改附件组件", "detail": "attachments.go"}}},
		{"id": "checked", "role": "assistant", "phase": "commentary", "text": "检查已通过，正在完成更新。"},
		{"id": "reason", "role": "reasoning", "text": "PRIVATE_REASONING_SENTINEL"},
	}
	if final {
		items = append(items, Object{"id": "answer", "role": "assistant", "phase": "final_answer", "text": "已完成。附件下载支持悬停显示、高亮和提示。"})
	}
	turn := Object{"id": "t", "status": status, "items": items}
	if status == "completed" {
		turn["durationMs"] = int64(135000)
	}
	return Object{"turns": []Object{turn}}
}

func TestWorkDurationLabelsAndProgressiveMetadata(t *testing.T) {
	for _, tc := range []struct {
		ms     *int64
		zh, en string
	}{
		{nil, "处理过程", "Work details"},
		{new(int64(-1)), "处理过程", "Work details"},
		{new(int64(0)), "已处理不到 1 秒", "Worked for less than a second"},
		{new(int64(999)), "已处理不到 1 秒", "Worked for less than a second"},
		{new(int64(12000)), "已处理 12 秒", "Worked for 12s"},
		{new(int64(135000)), "已处理 2 分 15 秒", "Worked for 2m 15s"},
		{new(int64(3723000)), "已处理 1 小时 2 分 3 秒", "Worked for 1h 2m 3s"},
	} {
		if got := workSummaryTitle(locales.In("zh-Hans"), tc.ms); got != tc.zh {
			t.Errorf("Chinese duration = %q, want %q", got, tc.zh)
		}
		if got := workSummaryTitle(locales.In("en"), tc.ms); got != tc.en {
			t.Errorf("English duration = %q, want %q", got, tc.en)
		}
	}
	var preview transcriptPreview
	if err := preview.add(provider.Transcript{Turns: []provider.Turn{{ID: "t", Status: "completed", DurationMS: new(int64(135000))}}}); err != nil {
		t.Fatal(err)
	}
	if number(preview.turns[0], "durationMs") != 135000 {
		t.Fatal("progressive preview dropped duration")
	}
}

func TestWorkSummaryPreservesIncompleteUnknownAndSteeredReplies(t *testing.T) {
	for _, status := range []string{"inProgress", "completed", "failed", "interrupted", "unknown"} {
		rows := messages(workSummaryTranscript(status, false))
		if !slices.ContainsFunc(rows, func(m Message) bool { return m.ID == "t|checked" }) {
			t.Fatalf("only available response disappeared for %s", status)
		}
		if slices.ContainsFunc(rows, func(m Message) bool { return m.Role == "work" }) {
			t.Fatal("folded process without a final answer")
		}
	}
	transcript := workSummaryTranscript("completed", true)
	turn := objects(transcript["turns"])[0]
	items := objects(turn["items"])
	steer := Object{"id": "steer", "role": "user", "text": "再检查一次"}
	unknown := Object{"id": "unknown", "role": "assistant", "phase": "future_phase", "text": "Unclassified content"}
	turn["items"] = append(append(append([]Object{}, items[:3]...), steer, unknown), items[3:]...)
	rows := messages(transcript)
	want := []string{"t|u", "t|work|t|progress", "t|steer", "t|unknown", "t|answer"}
	if len(rows) != len(want) {
		t.Fatalf("changed conversation boundaries: %+v", rows)
	}
	for i, row := range rows {
		if row.ID != want[i] {
			t.Fatalf("row %d: %s, want %s", i, row.ID, want[i])
		}
	}
	if rows[1].DurationMS == nil || *rows[1].DurationMS != 135000 || len(rows[1].Details) != 3 {
		t.Fatal("turn-wide duration or process details were lost")
	}
}

func TestWorkSummaryAfterTerminalEventAndLateFinalItem(t *testing.T) {
	source := workSummaryTranscript("inProgress", false)
	completed, _ := applyEvents(source, []Object{{"kind": "turn", "turnId": "t", "status": "completed"}})
	if slices.ContainsFunc(messages(completed), func(m Message) bool { return m.Role == "work" }) {
		t.Fatal("completion before final answer hid available progress")
	}
	completed, _ = applyEvents(completed, []Object{{"kind": "item", "turnId": "t", "itemId": "answer", "item": Object{"id": "answer", "role": "assistant", "phase": "final_answer", "text": "Complete final response"}}})
	rows := messages(completed)
	if len(rows) != 3 || rows[1].Role != "work" || rows[2].Text != "Complete final response" {
		t.Fatalf("late authoritative answer lost: %+v", rows)
	}
	preview := navigationPreview(rows, messageNavigationTarget{id: "t|u", row: 0})
	if preview.response != "Complete final response" {
		t.Fatal("navigation preview used process commentary", preview)
	}
}

func TestWorkSummaryKeepsPendingSubmissionAnchor(t *testing.T) {
	transcript := workSummaryTranscript("completed", true)
	s := &State{target: "fixture", selected: Object{"id": "s", "providerId": "codex-app"}, transcript: transcript}
	s.outgoing = map[string][]*outgoingMessage{s.key(): {{ID: "pending", Text: "Follow-up", Status: "uncertain", Anchors: []string{"t|checked", "t|u"}}}}
	s.rebuildMessages()
	if len(s.messageRows) != 4 || s.messageRows[1].Role != "work" || s.messageRows[2].ID != "pending" || s.messageRows[3].ID != "t|answer" {
		t.Fatal("folding moved an unconfirmed submission away from its predecessor", s.messageRows)
	}
}

func TestFinalItemCanCorrectTextWithoutResurrectingLateDeltas(t *testing.T) {
	source := Object{"turns": []Object{{"id": "t", "status": "inProgress", "items": []Object{{"id": "a", "role": "assistant", "phase": "final_answer", "status": "inProgress", "text": "Ready to use"}}}}}
	item := func(status, text string) Object {
		return Object{"kind": "item", "turnId": "t", "itemId": "a", "item": Object{"id": "a", "role": "assistant", "phase": "final_answer", "status": status, "text": text}}
	}
	done, _ := applyEvents(source, []Object{item("completed", "Ready"), {"kind": "turn", "turnId": "t", "status": "completed"}})
	if messages(done)[0].Text != "Ready" {
		t.Fatal("final item could not correct a longer streamed prefix")
	}
	done, _ = applyEvents(done, []Object{item("inProgress", "Ready to use"), {"kind": "text", "turnId": "t", "itemId": "a", "offset": 5, "delta": " to use"}})
	if messages(done)[0].Text != "Ready" {
		t.Fatal("late partial content overwrote the final answer")
	}
	done, _ = applyEvents(done, []Object{item("completed", "Ready now")})
	if messages(done)[0].Text != "Ready now" {
		t.Fatal("authoritative terminal correction was ignored")
	}
}

func TestWorkSummaryCompletionLight(t *testing.T) { testWorkSummaryCompletion(t, false) }
func TestWorkSummaryCompletionDark(t *testing.T)  { testWorkSummaryCompletion(t, true) }

func testWorkSummaryCompletion(t *testing.T, dark bool) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		s.applyColors()
		s.target, s.selected = "fixture", Object{"id": "s", "providerId": "codex-app"}
		s.setTranscript(workSummaryTranscript("inProgress", true))
		return core.View{Style: fill.Merge(core.Style{Padding: core.All(24)}), StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{
			button(cx, "finish", "Complete", nil, "ghost", func() { s.setTranscript(workSummaryTranscript("completed", true)); s.changed() }),
			core.Each(func() []Message { s.read(); return s.messageRows }, func(m Message) string { return m.ID }, func(cx *core.Scope, get func() Message, _ func() int) core.Node { return s.message(cx, get) }),
		}}
	}, nsgotest.Size(820, 900), nsgotest.Environment(core.Environment{Dark: dark, Locale: "zh-CN", OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	app.Find("text:正在调整附件下载交互。")
	answer := app.Find("id:message-t|answer").ID
	app.Press("id:finish")
	toggle := "id:work-toggle-t|work|t|progress"
	app.WaitFor(toggle, time.Second)
	app.Find("text:已处理 2 分 15 秒")
	toggleID := app.Find(toggle).ID
	if app.Exists("text:正在调整附件下载交互。") || app.Exists("text:检查已通过，正在完成更新。") || app.Find("id:message-t|answer").ID != answer {
		t.Fatal("completion left progress expanded or remounted the final answer")
	}
	app.Screenshot(fmt.Sprintf("completed-answer-dark-%t", dark))
	app.Press(toggle)
	app.WaitFor("text:正在调整附件下载交互。", time.Second)
	app.WaitFor("text:检查已通过，正在完成更新。", time.Second)
	if app.Exists("text:RAW_OUTPUT_SENTINEL") || app.Exists("text:PRIVATE_REASONING_SENTINEL") {
		t.Fatal("work details exposed raw execution data")
	}
	app.Post(func() {
		s.cx.Post(func() {
			updated, _ := applyEvents(s.transcript, []Object{{"kind": "turn", "turnId": "t", "status": "completed", "durationMs": int64(136000)}})
			s.setTranscript(updated)
			s.changed()
		})
	})
	app.Settle()
	app.Find("text:已处理 2 分 16 秒")
	if !app.Exists("text:正在调整附件下载交互。") || app.Find("id:message-t|answer").ID != answer || app.Find(toggle).ID != toggleID {
		t.Fatal("refresh reset expanded process or final answer")
	}
	app.Screenshot(fmt.Sprintf("completed-work-expanded-dark-%t", dark))
	app.Press(toggle)
	if app.Exists("text:正在调整附件下载交互。") {
		t.Fatal("process did not collapse")
	}
}

func TestSteeringUsesOneWorkSummaryAndKeepsPendingOrder(t *testing.T) {
	transcript := workSummaryTranscript("completed", true)
	turn := objects(transcript["turns"])[0]
	items := objects(turn["items"])
	turn["items"] = []Object{items[0], items[1], items[2],
		{"id": "steer1", "role": "user", "text": "补充第一条说明"}, items[3],
		{"id": "steer2", "role": "user", "text": "补充第二条说明", "attachments": []Object{{"id": "file", "kind": "file", "name": "notes.txt"}}},
		{"id": "later", "role": "assistant", "phase": "commentary", "text": "已收到补充，正在检查。"}, items[len(items)-1],
	}
	s := &State{target: "fixture", selected: Object{"id": "s", "providerId": "codex-app"}, transcript: transcript}
	s.outgoing = map[string][]*outgoingMessage{s.key(): {{ID: "pending", Text: "尚未确认的补充", Status: "uncertain", Anchors: []string{"t|later", "t|steer2"}}}}
	s.rebuildMessages()
	want := []string{"t|u", "t|work|t|progress", "t|steer1", "t|steer2", "pending", "t|answer"}
	if len(s.messageRows) != len(want) {
		t.Fatal("steers created extra summaries", s.messageRows)
	}
	for i, id := range want {
		if s.messageRows[i].ID != id {
			t.Fatal("pending steer moved across a confirmed user message", s.messageRows)
		}
	}
	if len(s.messageRows[3].Attachments) != 1 || len(s.messageRows[1].Details) != 4 {
		t.Fatal("lost attachment or process details")
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		state := newState(cx, newDemoAPI())
		state.applyColors()
		state.target, state.selected = "fixture", s.selected
		state.setTranscript(transcript)
		return core.Scroll{Style: fill, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{core.View{Style: core.Style{Padding: core.All(24)}, Children: []core.Node{
			core.Each(func() []Message { return state.messageRows }, func(m Message) string { return m.ID }, func(cx *core.Scope, get func() Message, _ func() int) core.Node { return state.message(cx, get) }),
		}}}}
	}, nsgotest.Size(900, 850), nsgotest.Environment(core.Environment{Dark: true, OS: core.OSMacOS, Locale: "zh-CN"}))
	app.Find("text:已处理 2 分 15 秒")
	app.Find("id:message-t|steer1")
	app.Find("id:message-t|steer2")
	app.Find("id:message-t|answer")
	app.Screenshot("steering-single-work-summary")
	app.Press("id:work-toggle-t|work|t|progress")
	app.Find("text:正在调整附件下载交互。")
	app.Find("text:已收到补充，正在检查。")
}

func TestCompactionAppearsBetweenProcessGroups(t *testing.T) {
	turn := Object{"id": "turn", "status": "inProgress", "items": []Object{
		{"id": "cmd", "role": "tool", "type": "commandExecution"},
		{"id": "compact", "role": "system", "type": "contextCompaction", "text": "PRIVATE_SUMMARY_SENTINEL"},
		{"id": "cmd2", "role": "tool", "type": "commandExecution"},
		{"id": "reply", "role": "assistant", "text": "继续处理下一条消息。"},
	}}
	transcript := Object{"turns": []Object{turn}}
	app, s := conversationFixture(t, newDemoAPI(), 900, transcript, true)
	app.Find("id:message-turn|compact")
	app.Post(func() {
		s.cx.Post(func() {
			final := copyObject(turn)
			final["status"] = "completed"
			s.setTranscript(Object{"turns": []Object{final}})
		})
	})
	app.WaitFor("id:work-toggle-turn|work|turn|tools|cmd2", time.Second)
	before := globalBounds(app.Find("id:work-toggle-turn|work|turn|tools|cmd")).Origin.Y
	marker := globalBounds(app.Find("id:message-turn|compact")).Origin.Y
	after := globalBounds(app.Find("id:work-toggle-turn|work|turn|tools|cmd2")).Origin.Y
	if !(before < marker && marker < after) || app.Exists("text:PRIVATE_SUMMARY_SENTINEL") {
		t.Fatal("compaction moved into a disclosure or exposed its payload")
	}
	app.Screenshot("conversation-context-compaction")
}
