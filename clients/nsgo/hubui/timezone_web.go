//go:build js && wasm

package hubui

import (
	"syscall/js"
	"time"
)

func platformLocalTime(t time.Time) time.Time {
	// Ask Date for this instant, preserving DST and browser timezone overrides.
	minutes := js.Global().Get("Date").New(float64(t.UnixMilli())).Call("getTimezoneOffset").Int()
	return t.In(time.FixedZone("Browser", -minutes*60))
}
func (s *State) refreshTimezone() { s.changed() }
