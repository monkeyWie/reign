//go:build js && wasm

package hubui

import (
	"syscall/js"

	"github.com/nsgo-dev/nsgo/core"
)

// Screen Wake Lock needs a secure context and browser support. Denial (for
// example battery saver) must not block reading or produce a retry loop.
func screenAwake(cx *core.Scope) func(bool) {
	lock := js.Global().Get("navigator").Get("wakeLock")
	if !lock.Truthy() || !js.Global().Get("isSecureContext").Truthy() {
		return func(bool) {}
	}
	doc := js.Global().Get("document")
	wanted, pending, closed := false, false, false
	generation := 0
	sentinel := js.Undefined()
	release := func() {
		if sentinel.Truthy() {
			// release() normally resolves; consume rejection if the browser has
			// already revoked the hold during a visibility transition.
			js.Global().Get("Promise").Call("allSettled", []any{sentinel.Call("release")})
			sentinel = js.Undefined()
		}
	}
	var acquire func()
	acquire = func() {
		if closed || !wanted || pending || doc.Get("visibilityState").String() != "visible" || sentinel.Truthy() && !sentinel.Get("released").Bool() {
			return
		}
		pending = true
		request := generation
		var success, failure js.Func
		settled := func() { pending = false; success.Release(); failure.Release() }
		success = js.FuncOf(func(_ js.Value, args []js.Value) any {
			settled()
			sentinel = args[0]
			if closed || !wanted || request != generation || doc.Get("visibilityState").String() != "visible" {
				release()
				// Navigation may leave and reenter while request() is pending.
				acquire()
			}
			return nil
		})
		failure = js.FuncOf(func(js.Value, []js.Value) any { settled(); return nil })
		lock.Call("request", "screen").Call("then", success, failure)
	}
	retry := js.FuncOf(func(js.Value, []js.Value) any { acquire(); return nil })
	doc.Call("addEventListener", "visibilitychange", retry)
	doc.Call("addEventListener", "pointerdown", retry)
	cx.Cleanup(func() {
		closed, wanted = true, false
		generation++
		release()
		doc.Call("removeEventListener", "visibilitychange", retry)
		doc.Call("removeEventListener", "pointerdown", retry)
		retry.Release()
	})
	return func(enabled bool) {
		if wanted == enabled {
			return
		}
		wanted = enabled
		generation++
		if enabled {
			acquire()
		} else {
			release()
		}
	}
}
