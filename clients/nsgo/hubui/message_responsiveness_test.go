package hubui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestLargeHistoryKeepsMessageControlsResponsive(t *testing.T) {
	turns := make([]Object, 512)
	for i := range turns {
		turns[i] = Object{"id": fmt.Sprint(i), "status": "completed", "items": []Object{
			{"id": "u", "role": "user", "text": fmt.Sprintf("Synthetic instruction %d", i)},
			{"id": "a", "role": "assistant", "text": strings.Repeat("Historical reply with **formatting** and a [link](https://example.com).\n\n", 30)},
		}}
	}
	turns = append(turns, Object{"id": "latest", "status": "inProgress", "items": []Object{
		{"id": "u", "role": "user", "text": "Compare platform support"},
		{"id": "a", "role": "assistant", "text": "Current support:\n\n| Platform | Local terminal |"},
	}})
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, newDemoAPI())
		s.width.Set(1000)
		s.devices = []Object{{"id": "demo-device", "connected": true}}
		s.target, s.selected = "demo-device", Object{"id": "demo-chat", "providerId": "codex-app"}
		s.setTranscript(Object{"turns": turns})
		return s.workspaceView(cx)
	}, nsgotest.Size(1000, 844))
	app.Type("id:chat-input", "Keep this draft")
	app.Press("id:attachment-cache")
	app.WaitFor("id:dialog-clear-cache", time.Second)
	app.Press("id:dialog-clear-cache-close")
	for _, suffix := range []string{"\n| -", "-- | --- |", "\n| Web | No |", "\n| Desktop | Yes |"} {
		app.Post(func() {
			s.cx.Post(func() {
				item := objects(turns[len(turns)-1]["items"])[1]
				item["text"] = str(item, "text") + suffix
				s.rebuildMessages()
				s.changed()
			})
		})
		app.Settle()
		app.Press("id:attachment-cache")
		app.WaitFor("id:dialog-clear-cache", time.Second)
		app.Press("id:dialog-clear-cache-close")
	}
	app.Press("id:message-tick-0|u")
	app.WaitFor("id:jump-to-latest", time.Second)
	app.Press("id:jump-to-latest")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:jump-to-latest") })
	if app.Find("id:chat-input").Text != "Keep this draft" {
		t.Fatal("history updates lost the draft")
	}
}
