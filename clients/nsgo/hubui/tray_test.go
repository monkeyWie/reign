package hubui

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestTrayShortcutsRestoreWindowAndKeepConversation(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("records desktop menu callbacks with the simulated platform")
	}
	var state *State
	shows := 0
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		state = newState(cx, newDemoAPI())
		state.target, state.selected = "paired-device", Object{"id": "saved-conversation"}
		state.draft.Set("未发送的草稿")
		buttons := []core.Node{}
		for _, item := range state.trayMenu(func() { shows++ }) {
			if !item.Separator {
				buttons = append(buttons, button(cx, item.Label, item.Label, nil, "ghost", item.OnSelect))
			}
		}
		return core.View{Children: buttons}
	})
	for _, label := range []string{"显示 ReignHub", "连接设备…", "本机 Agent…", "设置…"} {
		app.Press("id:" + label)
	}
	if shows != 4 || state.target != "paired-device" || str(state.selected, "id") != "saved-conversation" || state.draft.GetUntracked() != "未发送的草稿" {
		t.Fatal("tray navigation lost the window state")
	}
	if state.dialog.GetUntracked() != "settings:general" {
		t.Fatal("settings shortcut did not select general settings")
	}
	app.Press("id:退出 ReignHub")
	if app.Quits != 1 {
		t.Fatal("tray quit did not terminate explicitly")
	}
}
