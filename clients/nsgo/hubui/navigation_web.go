//go:build js && wasm

package hubui

import "syscall/js"

const workspaceHistoryKey = "reignhubWorkspace"

// The console mounts NSGo in an element so it can show its lightweight boot
// screen. NSGo deliberately leaves browser history to an embedded app's host.
// Keep a single boundary entry and route each browser Back through NSGo's
// regular Back event, including dialog and navigation-drawer dismissal.
// Browsers cannot erase a tab's previous history; the boundary keeps ordinary
// Back inside the workspace without adding entries on every render or reload.
func (s *State) installWorkspaceNavigation() {
	win := js.Global().Get("window")
	if !win.Get("top").Equal(win) {
		return
	}
	s.cx.OnBackPress(s.workspaceBack)
	history := win.Get("history")
	marker := func(value string) map[string]any { return map[string]any{workspaceHistoryKey: value} }
	state := history.Get("state")
	if !state.Truthy() || state.Get(workspaceHistoryKey).String() != "active" {
		history.Call("replaceState", marker("root"), "")
		history.Call("pushState", marker("active"), "")
	}
	back := js.FuncOf(func(_ js.Value, args []js.Value) any {
		state := args[0].Get("state")
		if state.Truthy() && state.Get(workspaceHistoryKey).String() == "active" {
			return nil
		}
		history.Call("pushState", marker("active"), "")
		// Dispatch to NSGo instead of modifying State here: open menus, image
		// previews and dialogs must get the same Back priority as native apps.
		event := win.Get("KeyboardEvent").New("keydown", map[string]any{"key": "BrowserBack", "bubbles": true, "cancelable": true})
		win.Get("document").Call("dispatchEvent", event)
		return nil
	})
	win.Call("addEventListener", "popstate", back)
	s.cx.Cleanup(func() {
		win.Call("removeEventListener", "popstate", back)
		back.Release()
	})
}

func openWebsite() { js.Global().Get("location").Call("assign", "/") }
