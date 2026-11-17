package hubui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestNavigationPreviewMatchesReplyInConversationOrder(t *testing.T) {
	rows := []Message{
		{ID: "answer-b", Role: "assistant", Text: "Second reply", Turn: "b"},
		{ID: "user-b", Role: "user", Text: "Second instruction", Turn: "b"},
		{ID: "later-a", Role: "assistant", Text: "Later reply", Turn: "a"},
		{ID: "answer-a", Role: "assistant", Text: "First reply", Turn: "a"},
		{ID: "tool", Role: "tool", Text: "Internal tool output", Turn: "a"},
		{ID: "user-a", Role: "user", Text: "First instruction", Turn: "a"},
	}
	targets := messageNavigationTargets(rows, 600)
	if len(targets) != 2 || targets[0].id != "user-a" || targets[1].id != "user-b" {
		t.Fatal("navigation is not in chronological order")
	}
	p := navigationPreview(rows, targets[0])
	if p.instruction != "First instruction" || p.response != "First reply" {
		t.Fatalf("wrong preview: %+v", p)
	}
	for _, rows := range [][]Message{
		{{ID: "new-answer", Role: "assistant", Text: "Unrelated", Turn: "b"}, {ID: "u", Role: "user", Turn: "a", Attachments: []Object{{"name": "image.png"}}}},
		{{ID: "answer", Role: "assistant", Text: "Unrelated"}, {ID: "new-user", Role: "user"}, {ID: "u", Role: "user"}},
	} {
		p := navigationPreview(rows, messageNavigationTarget{"u", len(rows) - 1})
		if p.response != "" || p.instruction == "" {
			t.Fatalf("missing response borrowed another instruction's reply: %+v", p)
		}
	}
	if got := navigationSnippet("  首行\n 第二行\t第三行末尾", 8); got != "首行 第二行 第三行…" {
		t.Fatalf("unexpected bounded snippet: %q", got)
	}
}

func TestMessageNavigationPreviewAndWaveLight(t *testing.T) {
	testMessageNavigationPreviewAndWave(t, false)
}
func TestMessageNavigationPreviewAndWaveDark(t *testing.T) {
	testMessageNavigationPreviewAndWave(t, true)
}

func TestMessageNavigationJumpAfterNewTurnAndApproval(t *testing.T) {
	turns := []Object{}
	for i := range 64 {
		turns = append(turns, Object{"id": fmt.Sprint(i), "status": "completed", "items": []Object{
			{"id": "u" + fmt.Sprint(i), "role": "user", "text": "Instruction " + fmt.Sprint(i)},
			{"id": "a" + fmt.Sprint(i), "role": "assistant", "text": strings.Repeat("A long response to scroll past.\n\n", 8)},
		}})
	}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		s.applyColors()
		s.width.Set(1100)
		s.target, s.selected = "demo-device", Object{"id": "demo-chat", "providerId": "codex-app", "title": "Fixture"}
		s.devices = []Object{{"id": "demo-device", "connected": true}}
		s.setTranscript(Object{"turns": turns})
		return s.conversation(cx, false)
	}, nsgotest.Size(1100, 700))
	app.WaitFor("id:message-tick-0|u0", time.Second)
	tick := app.Find("id:message-tick-0|u0").ID
	app.Post(func() {
		s.cx.Post(func() {
			turns = append(append([]Object{}, turns...), Object{"id": "new", "status": "inProgress", "items": []Object{{"id": "new-user", "role": "user", "text": "New instruction"}}})
			s.setTranscript(Object{"turns": turns})
			s.approvals = []Object{{"id": "fixture", "params": Object{"title": "Awaiting approval"}}}
			s.changed()
		})
	})
	app.WaitFor("id:message-tick-new|new-user", time.Second)
	if app.Find("id:message-tick-0|u0").ID != tick {
		t.Fatal("new turn remounted the previous target")
	}
	app.Press("id:message-tick-0|u0")
	// Even a distant unmounted row must appear without traversing the history.
	app.WaitFor("id:message-0|u0", 250*time.Millisecond)
	app.WaitFor("id:jump-to-latest", time.Second)
}

func testMessageNavigationPreviewAndWave(t *testing.T, dark bool) {
	var owner *core.Scope
	var source *core.Signal[[]Message]
	messages := []Message{}
	for i := 7; i >= 0; i-- {
		messages = append(messages,
			Message{ID: fmt.Sprintf("a%d", i), Turn: fmt.Sprint(i), Role: "assistant", Text: "Agent reply begins here. " + strings.Repeat("Long content to truncate. ", 1000)},
			Message{ID: fmt.Sprintf("u%d", i), Turn: fmt.Sprint(i), Role: "user", Text: fmt.Sprintf("Instruction %d: ", i) + strings.Repeat("A long instruction. ", 20)})
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		owner = cx
		s := &State{cx: cx}
		s.applyColors()
		source = core.NewSignalFunc(cx, messages, nil)
		rows := core.NewMemoFunc(cx, source.Get, nil)
		viewport := core.NewSignal(cx, core.Size{Width: 800, Height: 600})
		return core.View{Style: fill, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{s.messageNavigation(cx, rows, &core.Ref{}, viewport, func() int { return 0 })}}
	}, nsgotest.Size(800, 600), nsgotest.Environment(core.Environment{Dark: dark, OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	id := "id:message-tick-u3"
	tickID := app.Find(id).ID
	app.HoverAt(id, core.Point{X: 16, Y: 6})
	app.Advance(80 * time.Millisecond)
	app.WaitFor("id:message-navigation-preview", time.Second)
	if !strings.HasPrefix(app.Find("id:navigation-instruction").Text, "Instruction 3:") || !strings.HasPrefix(app.Find("id:navigation-response").Text, "Agent reply begins here.") {
		t.Fatal("tooltip did not show instruction and matching reply")
	}
	app.Advance(160 * time.Millisecond)
	peak := app.Find(id + "-bar").Style.Transform.ScaleX
	neighbor := app.Find("id:message-tick-u2-bar").Style.Transform.ScaleX
	far := app.Find("id:message-tick-u7-bar").Style.Transform.ScaleX
	if peak <= neighbor || neighbor <= far || far < 1 {
		t.Fatalf("no neighboring wave: peak=%g neighbor=%g far=%g", peak, neighbor, far)
	}
	if globalBounds(app.Find("id:navigation-response")).Size.Height > 60 {
		t.Fatal("reply preview exceeded three lines")
	}
	app.Advance(time.Second)

	previewID := app.Find("id:message-navigation-preview").ID
	next := append([]Message{}, messages...)
	next[8].Text = "Streaming reply updated"
	app.Post(func() { owner.Post(func() { source.Set(next) }) })
	app.WaitFor("text:Streaming reply updated", time.Second)
	if app.Find(id).ID != tickID || app.Find("id:message-navigation-preview").ID != previewID {
		t.Fatal("streaming updates remounted navigation or its open preview")
	}

	if !app.Exists("id:message-navigation-preview") {
		t.Fatal("preview closed during sustained hover")
	}
	app.Key("tab")
	app.WaitFor("id:message-navigation-preview", time.Second)
	app.Screenshot(fmt.Sprintf("message-navigation-preview-dark-%t", dark))

	app.Key("escape")
	app.HoverOut()
	app.Advance(180 * time.Millisecond)
	if app.Exists("id:message-navigation-preview") || app.Find(id+"-bar").Style.Transform.ScaleX != 1 {
		t.Fatal("navigation did not settle after pointer left")
	}
}
