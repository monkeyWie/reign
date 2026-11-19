package hubui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"testing"
	"time"
)

func TestSettingsThemePreservesWorkspace(t *testing.T) {
	app := startDemo(t, 1320, 880)
	app.WaitFor("id:session-sidebar", 3*time.Second)
	if app.Exists("id:add-agent") || app.Exists("id:configure-agent-claude-code") {
		t.Fatal("Agent controls remain on dashboard")
	}
	openDemoChat(t, app)
	app.Type("id:chat-input", "设置切换不丢草稿")
	sidebarMenuAction(app, "server-settings")
	app.Find("id:appearance-settings")
	app.Press("id:appearance-select")
	app.Press("role:option 明亮")
	app.Screenshot("settings-light")
	app.Press("id:appearance-select")
	app.Press("role:option 暗黑")
	app.Screenshot("settings-dark")
	app.Press("id:settings-close")
	if text := app.Find("id:chat-input").Text; text != "设置切换不丢草稿" {
		t.Fatalf("theme lost draft: %q", text)
	}
	app.Screenshot("workspace-new-dark")
	sidebarMenuAction(app, "theme-toggle")
	app.Screenshot("workspace-new-light")
	sidebarMenuAction(app, "server-settings")
	app.Press("id:settings-nav-agents")
	app.WaitFor("id:agent-settings-list", 3*time.Second)
	app.Press("id:add-agent")
	app.Find("id:dialog-agent-config")
	app.Press("id:dialog-cancel")
	app.Find("id:agent-settings-list")
	app.Screenshot("settings-agents")
	app.Press("id:settings-nav-connection")
	app.Press("id:settings-service")
	app.Find("id:server-url")
	app.Press("id:dialog-cancel")
	app.Find("id:settings-service")
	app.Press("id:settings-close")
}
func TestSettingsNarrowNavigationAndSearch(t *testing.T) {
	app := startDemo(t, 390, 844)
	app.WaitFor("id:sidebar-menu", 3*time.Second)
	sidebarMenuAction(app, "server-settings")
	app.Type("id:settings-search", "Agent")
	app.Find("id:settings-nav-agents")
	if app.Exists("id:settings-nav-general") {
		t.Fatal("search did not filter menu")
	}
	app.Press("id:settings-nav-agents")
	app.WaitFor("id:configure-agent-claude-code", 3*time.Second)
	for _, id := range []string{"settings-content", "agent-settings-list", "add-agent", "configure-agent-claude-code"} {
		b := globalBounds(app.Find("id:" + id))
		if b.Origin.X < 0 || b.Origin.X+b.Size.Width > 390.5 {
			t.Fatalf("%s overflows: %v", id, b)
		}
	}
	app.Screenshot("settings-narrow-agents")
	app.Press("id:settings-back")
	app.Clear("id:settings-search")
	app.Press("id:settings-nav-general")
	app.Screenshot("settings-narrow-appearance")
	app.Press("id:settings-close")
	app.Find("id:session-sidebar")
}
func TestAppearancePreferencesPersist(t *testing.T) {
	var state *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		state = newState(cx, newDemoAPI())
		state.loadAppearance()
		return button(cx, "save-preferences", "设置外观", nil, "ghost", func() {
			state.setAppearance("dark")
		})
	}, nsgotest.Size(390, 844))
	app.Press("id:save-preferences")
	app.Post(func() {
		state.cx.Post(func() {
			next := newState(state.cx, newDemoAPI())
			next.loadAppearance()
			if next.appearance != "dark" || !next.cx.Dark() {
				t.Error("appearance preferences did not persist")
			}
		})
	})
	app.Settle()
}
