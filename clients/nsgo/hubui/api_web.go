//go:build js && wasm

package hubui

import (
	"github.com/monkeyWie/reignhub/clients/nsgo/webapi"
	"github.com/monkeyWie/reignhub/internal/device"
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

func platformClientInfo() device.ClientInfo {
	nav := js.Global().Get("navigator")
	ua := nav.Get("userAgent").String()
	info := device.ClientInfo{Name: browserDeviceName(ua), Platform: browserPlatform(ua), Kind: "browser"}
	if info.Platform == "darwin" && nav.Get("maxTouchPoints").Type() == js.TypeNumber && nav.Get("maxTouchPoints").Int() > 1 {
		info.Platform = "ios"
	}
	// Client hints identify Chromium derivatives without exposing the OS in the name.
	if data := nav.Get("userAgentData"); data.Type() == js.TypeObject {
		if brands := data.Get("brands"); brands.Type() == js.TypeObject {
			for _, name := range []string{"Microsoft Edge", "Opera", "Google Chrome", "Chromium"} {
				for j := 0; j < brands.Length(); j++ {
					b := brands.Index(j)
					if b.Get("brand").String() == name {
						version := strings.Split(b.Get("version").String(), ".")[0]
						if version != "" && strings.Trim(version, "0123456789") == "" {
							info.Name = name + " " + version
							return info.Normalize()
						}
					}
				}
			}
		}
	}
	return info.Normalize()
}
func platformDeviceName() string                                    { return platformClientInfo().Name }
func resolveClientInfo(_ *core.Scope, done func(device.ClientInfo)) { done(platformClientInfo()) }
