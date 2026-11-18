package hubui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestConversationToolbarDetailsPreserveWorkspace(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 820), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	header := globalBounds(app.Find("id:conversation-header"))
	toggle := globalBounds(app.Find("id:toggle-sidebar"))
	timeline := globalBounds(app.Find("id:timeline"))
	if math.Abs(float64(header.Origin.Y+header.Size.Height/2-toggle.Origin.Y-toggle.Size.Height/2)) > 1 || timeline.Origin.Y != header.Origin.Y+header.Size.Height {
		t.Fatal("conversation heading does not share the window toolbar", header, toggle, timeline)
	}
	if app.Exists(`text:"/work/design"`) {
		t.Fatal("working path remained in the resting toolbar")
	}
	if app.Exists("id:connection-status-local") {
		t.Fatal("local conversation exposes a remote connection indicator")
	}
	for _, id := range []string{"conversation-title-control", "conversation-toolbar-actions", "attachment-cache"} {
		if app.Find("id:"+id).Props.WindowDrag != core.WindowDragOff {
			t.Fatal("toolbar control can drag the window", id)
		}
	}
	app.Type("id:chat-input", "Keep the draft while inspecting details")
	input := app.Find("id:chat-input").ID
	app.Screenshot("conversation-toolbar-light")
	app.Press("id:conversation-details")
	app.Find(`text:"/work/design"`)
	panel := app.Find("id:conversation-info-panel").ID
	titleControl := app.Find("id:conversation-details").ID
	for i := 0; i < 3; i++ {
		app.Post(func() {
			s.cx.Post(func() {
				s.selected = copyObject(s.selected)
				s.selected["title"] = "更新标题时保持会话与面板"
				s.changed()
			})
		})
		app.Settle()
		if app.Find("id:conversation-info-panel").ID != panel || app.Find("id:conversation-details").ID != titleControl || app.Find("id:chat-input").ID != input {
			t.Fatal("metadata refresh replaced the disclosure or editor")
		}
	}
	app.Press("id:conversation-details")
	app.Press("id:theme-toggle")
	app.Press("id:conversation-details")
	app.Screenshot("conversation-details-dark")
	app.Press("id:attachment-cache")
	app.Find("id:dialog-clear-cache")
	app.Press("id:dialog-clear-cache-close")
	if app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "Keep the draft while inspecting details" {
		t.Fatal("toolbar action reset the editor")
	}
	app.Post(func() {
		s.cx.Post(func() {
			s.selected = copyObject(s.selected)
			s.selected["title"] = strings.Repeat("一个很长的会话标题", 12)
			s.changed()
		})
	})
	app.Resize(650, 820)
	app.Settle()
	titleBounds := globalBounds(app.Find("id:conversation-details"))
	actions := globalBounds(app.Find("id:conversation-toolbar-actions"))
	if titleBounds.Size.Width <= 0 || titleBounds.Origin.X+titleBounds.Size.Width > actions.Origin.X || actions.Origin.X+actions.Size.Width > 650 {
		t.Fatal("long title pushed toolbar actions outside the viewport", titleBounds, actions)
	}
	app.Screenshot("conversation-toolbar-narrow-long-title")
	app.Press("id:conversation-details")
	app.Find("id:conversation-info-panel")
	app.Press(sessionSelector("remote", "claude-code", "review"))
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:conversation-info-panel") })
	app.Find("id:connection-status-remote")
	app.Press("id:conversation-details")
	app.Find(`text:"/srv/reign"`)
	if app.Exists(`text:"/work/design"`) {
		t.Fatal("new session retained stale detail metadata")
	}
	app.Press("id:conversation-details")
	app.Resize(390, 844)
	app.WaitFor("id:back-sessions", time.Second)
	app.Find("id:conversation-header-fade")
	if app.Exists("id:conversation-details") {
		t.Fatal("wide toolbar remained in compact navigation")
	}
	app.Find("id:connection-status-remote")
	app.Press("id:back-sessions")
	localSession := "id:" + sessionRowID("pinned", Object{"deviceId": "local", "providerId": "codex-cli", "id": "design"})
	app.WaitFor(localSession, time.Second)
	app.Press(localSession)
	app.WaitFor("id:conversation-header-fade", time.Second)
	if app.Exists("id:connection-status-local") || app.Exists("id:connection-status-remote") {
		t.Fatal("compact local conversation retained a remote connection indicator")
	}
	app.Find("id:attachment-cache")
	app.Screenshot("conversation-toolbar-local-compact")
}
