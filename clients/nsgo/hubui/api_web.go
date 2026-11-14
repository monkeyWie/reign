//go:build js && wasm

package hubui

import (
	"github.com/monkeyWie/reignhub/clients/nsgo/webapi"
	"github.com/nsgo-dev/nsgo/core"
	"net/url"
	"strings"
	"syscall/js"
)

func platformAPI(*core.Scope) API { return webapi.New(&webapi.Store{}) }
func serviceOrigin() string {
	origin := js.Global().Get("location").Get("origin").String()
	if origin == "" || origin == "null" {
		return DefaultService
	}
	return origin
}
func demoRequested() bool {
	q, _ := url.ParseQuery(strings.TrimPrefix(js.Global().Get("location").Get("search").String(), "?"))
	return q.Get("demo") == "1"
}
func isWeb() bool { return true }

func platformDeviceName() string {
	return browserDeviceName(js.Global().Get("navigator").Get("userAgent").String())
}
func resolveDeviceName(_ *core.Scope, done func(string)) { done(platformDeviceName()) }
