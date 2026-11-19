//go:build js && wasm

package hubui

import (
	"syscall/js"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type browserNavigationAPI struct{ *demoAPI }

func (*browserNavigationAPI) CanHost() bool { return false }

func startBrowserNavigation(t *testing.T, width, height core.Points) *nsgotest.App {
	t.Helper()
	return nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return RootWithAPI(cx, &browserNavigationAPI{newDemoAPI()}, true)
	}, nsgotest.Size(width, height))
}

// Exercise real browser history, not App.Back (which bypasses popstate).
func pressWorkspaceBack(t *testing.T, app *nsgotest.App) {
	t.Helper()
	// App.Back is Escape on Web, which intentionally stays in a focused editor.
	pressBrowserBack(t, app)
}

func pressBrowserBack(t *testing.T, app *nsgotest.App) {
	t.Helper()
	win := js.Global().Get("window")
	popped := false
	listener := js.FuncOf(func(js.Value, []js.Value) any { popped = true; return nil })
	win.Call("addEventListener", "popstate", listener)
	defer func() { win.Call("removeEventListener", "popstate", listener); listener.Release() }()
	win.Get("history").Call("back")
	app.WaitUntil(3*time.Second, func() bool { return popped })
	app.Settle()
}

func testBrowserBackStaysInCompactWorkspace(t *testing.T) {
	app := startBrowserNavigation(t, 390, 844)
	win := js.Global().Get("window")
	url := win.Get("location").Get("href").String()
	length := win.Get("history").Get("length").Int()
	openDemoChat(t, app)
	app.Type("id:chat-input", "返回后保留的草稿")
	pressBrowserBack(t, app)
	app.WaitFor("id:session-sidebar", time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Text == "返回后保留的草稿" })
	// An in-app Back must not leave an extra browser step behind.
	app.Press("id:back-sessions")
	pressBrowserBack(t, app)
	app.WaitFor("id:session-sidebar", time.Second)
	for range 3 {
		pressBrowserBack(t, app)
		app.Find("id:session-sidebar")
		if app.Exists("id:chat-input") {
			t.Fatal("root Back reopened the conversation")
		}
	}
	if win.Get("location").Get("href").String() != url || win.Get("history").Get("length").Int() != length {
		t.Fatal("Back left the workspace or grew its boundary history")
	}
	app.Press("id:connect-device")
	app.Find("id:dialog-connect")
	pressBrowserBack(t, app)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:dialog-connect") })
	app.Find("id:session-sidebar")
	app.Find("role:button ReignHub 官网")
}

func testBrowserBackStaysInWideWorkspace(t *testing.T) {
	app := startBrowserNavigation(t, 1320, 880)
	openDemoChat(t, app)
	app.Type("id:chat-input", "桌面草稿")
	pressBrowserBack(t, app)
	app.Find("id:session-sidebar")
	if app.Exists("id:chat-input") {
		t.Fatal("wide Back did not close the selected conversation")
	}
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Text == "桌面草稿" })
	pressBrowserBack(t, app)
	pressBrowserBack(t, app)
	app.Find("id:welcome-connect")
	pressBrowserBack(t, app)
	app.Find("id:welcome-connect")
	app.Find("role:button ReignHub 官网")
}
