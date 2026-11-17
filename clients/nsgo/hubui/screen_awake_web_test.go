//go:build js && wasm

package hubui

import (
	"syscall/js"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func testBrowserScreenAwake(t *testing.T) {
	nav, object := js.Global().Get("navigator"), js.Global().Get("Object")
	descriptor := object.Call("getOwnPropertyDescriptor", nav, "wakeLock")
	var resolves, rejects []js.Value
	request := js.FuncOf(func(js.Value, []js.Value) any {
		executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
			resolves, rejects = append(resolves, args[0]), append(rejects, args[1])
			return nil
		})
		promise := js.Global().Get("Promise").New(executor)
		executor.Release()
		return promise
	})
	object.Call("defineProperty", nav, "wakeLock", map[string]any{"configurable": true, "value": map[string]any{"request": request}})
	t.Cleanup(func() {
		if descriptor.Truthy() {
			object.Call("defineProperty", nav, "wakeLock", descriptor)
		} else {
			js.Global().Get("Reflect").Call("deleteProperty", nav, "wakeLock")
		}
		request.Release()
	})
	releases := 0
	release := js.FuncOf(func(js.Value, []js.Value) any {
		releases++
		return js.Global().Get("Promise").Call("resolve")
	})
	t.Cleanup(release.Release)
	sentinel := map[string]any{"released": false, "release": release}
	var mounted *core.Signal[bool]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mounted = core.NewSignal(cx, true)
		return core.View{Children: []core.Node{
			button(cx, "dispose-awake", "Dispose", nil, "outline", func() { mounted.Set(false) }),
			core.Show{When: mounted.Get, Then: func(cx *core.Scope) core.Node {
				set := screenAwake(cx)
				return row(button(cx, "awake-on", "On", nil, "outline", func() { set(true) }), button(cx, "awake-off", "Off", nil, "outline", func() { set(false) }))
			}},
		}}
	})
	app.Press("id:awake-on")
	app.WaitUntil(time.Second, func() bool { return len(resolves) == 1 })
	app.Press("id:awake-off")
	resolves[0].Invoke(sentinel)
	app.WaitUntil(time.Second, func() bool { return releases == 1 })
	app.Press("id:awake-on")
	app.WaitUntil(time.Second, func() bool { return len(resolves) == 2 })
	rejects[1].Invoke(js.Global().Get("Error").New("Denied by battery policy"))
	app.Advance(time.Second)
	if len(resolves) != 2 {
		t.Fatal("denied wake lock entered an automatic retry loop")
	}
	app.Press("id:awake-on") // pointerdown retries a denied hold.
	app.WaitUntil(time.Second, func() bool { return len(resolves) == 3 })
	app.Press("id:dispose-awake")
	resolves[2].Invoke(sentinel)
	app.WaitUntil(time.Second, func() bool { return releases == 2 })
	app.Press("id:dispose-awake")
	if len(resolves) != 3 {
		t.Fatal("disposed wake-lock listener requested another hold")
	}
}
