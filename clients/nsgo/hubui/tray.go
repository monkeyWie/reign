package hubui

import (
	_ "embed"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// Both icons use the official website's rook mark. macOS uses an alpha
// template so the system keeps it legible on light and dark menu bars.
//
//go:embed assets/tray.png
var trayIcon []byte

//go:embed assets/tray-template.png
var trayTemplateIcon []byte

func (s *State) installDesktopTray(cx *core.Scope) {
	if !customWindow(cx) {
		return
	}
	win := desktop.CurrentWindow(cx)
	if win == nil {
		return
	}
	icon := trayIcon
	template := cx.Env().OS() == core.OSMacOS
	if template {
		icon = trayTemplateIcon
	}
	tray := desktop.NewTray(cx, desktop.TrayOptions{
		Icon: icon, Template: template, Tooltip: "ReignHub · 正在运行",
		OnClick: win.Focus,
		Menu:    func() []desktop.MenuItem { return s.trayMenu(win.Focus) },
	})
	if tray == nil {
		return
	}
	desktop.SetShutdownMode(cx, desktop.ShutdownOnExplicit)
	desktop.OnReopen(cx, win.Focus)
	win.OnClosing(cx, func(c *desktop.WindowClosing) {
		c.Cancel()
		win.Hide()
	})
}

func (s *State) trayMenu(show func()) []desktop.MenuItem {
	open := func(action func()) func() {
		return func() { show(); action() }
	}
	return []desktop.MenuItem{
		{Label: "显示 ReignHub", OnSelect: show},
		{Separator: true},
		{Label: "连接设备…", OnSelect: open(func() { s.scanned = nil; s.dialog.Set("connect") })},
		{Label: "本机 Agent…", OnSelect: open(func() { s.openSettings("agents") })},
		{Label: "设置…", OnSelect: open(func() { s.openSettings("general") })},
		{Separator: true},
		{Label: "退出 ReignHub", OnSelect: s.cx.Quit},
	}
}
