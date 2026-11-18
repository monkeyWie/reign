package hubui

import (
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

func browserIconKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, browser := range []struct{ name, key string }{
		{"microsoft edge", "edge"}, {"edge", "edge"},
		{"google chrome", "chrome"}, {"chrome", "chrome"},
		{"chromium", "chromium"}, {"firefox", "firefox"},
		{"safari", "safari"}, {"opera", "opera"},
		{"brave", "brave"}, {"vivaldi", "vivaldi"},
	} {
		if name == browser.name || strings.HasPrefix(name, browser.name+" ") || strings.HasPrefix(name, browser.name+"/") {
			return browser.key
		}
	}
	return "browser"
}
func controllerIconKey(info Object) string {
	kind := str(info, "kind")
	browser := browserIconKey(str(info, "name"))
	// Browser identity takes precedence over its underlying operating system.
	// Recognize older name-only grants, but never infer an OS from a hostname.
	if kind == "browser" || kind == "" && browser != "browser" {
		return browser
	}
	switch strings.ToLower(str(info, "platform")) {
	case "darwin", "macos", "ios":
		return "apple"
	case "windows", "android", "linux":
		return strings.ToLower(str(info, "platform"))
	}
	return "device"
}
func identityGlyph(cx *core.Scope, info Object, size core.Points) core.Node {
	key := controllerIconKey(info)
	if svg := brandLogos[key]; svg != nil {
		return core.Icon{SVG: svg, TestID: "controller-platform-" + key, Style: core.Style{Width: core.Pt(size), Height: core.Pt(size), FlexShrink: 0}, StyleFunc: func() core.Style { return core.Style{Color: palette(cx).foreground} }}
	}
	if key == "browser" {
		return glyph(cx, lucide.Globe, size)
	}
	return glyph(cx, lucide.Monitor, size)
}
func controllerIdentifier(info Object) string {
	if str(info, "kind") != "native" {
		return ""
	}
	switch str(info, "platform") {
	case "darwin", "windows", "linux":
		fingerprint := str(info, "fingerprint")
		if len(fingerprint) >= 8 {
			return "设备标识 · " + fingerprint[:4] + "-" + fingerprint[4:8]
		}
	}
	return ""
}
func controllerIdentity(cx *core.Scope, get func() Object) core.Node {
	return row(core.Dynamic(func(cx *core.Scope) core.Node {
		return identityGlyph(cx, get(), 22)
	}), core.View{Style: fill, Children: []core.Node{
		singleLine(cx, func() string { return str(get(), "name") }, 13, false),
		core.Show{When: func() bool { return controllerIdentifier(get()) != "" }, Then: func(cx *core.Scope) core.Node {
			return dynamicText(cx, func() string { return controllerIdentifier(get()) }, 11, true)
		}},
	}})
}
