package hubui

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/monkeyWie/reignhub/internal/device"
)

func browserDeviceName(ua string) string {
	for _, browser := range []struct {
		name   string
		tokens []string
	}{
		{"Microsoft Edge", []string{"Edg/", "EdgA/", "EdgiOS/"}},
		{"Opera", []string{"OPR/", "OPiOS/", "Opera/"}},
		{"Samsung Internet", []string{"SamsungBrowser/"}},
		{"Firefox", []string{"Firefox/", "FxiOS/"}},
		{"Chrome", []string{"Chrome/", "CriOS/"}},
		{"Safari", []string{"Version/"}},
	} {
		for _, token := range browser.tokens {
			if start := strings.Index(ua, token); start >= 0 {
				version := ua[start+len(token):]
				end := 0
				for end < len(version) && version[end] >= '0' && version[end] <= '9' {
					end++
				}
				if end > 0 {
					return browser.name + " " + version[:end]
				}
				return browser.name
			}
		}
	}
	if strings.Contains(ua, "Safari/") {
		return "Safari"
	}
	return "浏览器"
}

func browserPlatform(ua string) string {
	switch {
	case strings.Contains(ua, "Android"):
		return "android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "iPod"):
		return "ios"
	case strings.Contains(ua, "Windows"):
		return "windows"
	case strings.Contains(ua, "Macintosh"):
		return "darwin"
	case strings.Contains(ua, "Linux"):
		return "linux"
	}
	return ""
}

// Requests may run outside the UI thread. Publish immutable profile snapshots.
type identifiedAPI struct {
	API
	info atomic.Value
}

func (a *identifiedAPI) setInfo(info device.ClientInfo) { a.info.Store(info.Normalize()) }
func (a *identifiedAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	return a.API.Call(device.WithClientInfo(ctx, a.info.Load().(device.ClientInfo)), method, p, target, r)
}

func appleDeviceName(name, model string) string {
	name = strings.TrimSpace(name)
	switch name {
	case "", "iPhone", "iPad", "iPod touch", "iPhone / iPad":
		if friendly := appleModels[model]; friendly != "" {
			return friendly
		}
	}
	return name
}
