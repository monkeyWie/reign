//go:build !js || !wasm

package hubui

import "github.com/nsgo-dev/nsgo/core"

func screenAwake(cx *core.Scope) func(bool) {
	enabled := false
	var release func()
	set := func(wanted bool) {
		if wanted == enabled {
			return
		}
		enabled = wanted
		if cx.Env().OS() == core.OSAndroid {
			// NSGo's Android KeepAwake backend is currently a no-op. Use the
			// activity window flag; it needs no wake-lock permission.
			cx.CallHost("ras.screen-awake", Object{"enabled": wanted}, nil)
		} else if wanted {
			release = cx.KeepAwake("Reading a ReignHub conversation", core.KeepAwakeOptions{Display: true})
		} else if release != nil {
			release()
			release = nil
		}
	}
	cx.Cleanup(func() { set(false) })
	return set
}
