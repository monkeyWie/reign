package hubui

import (
	"github.com/monkeyWie/reignhub/internal/brandicons"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
)

// Use the same recognizable brand silhouettes as the website.
var brandLogos = func() map[string]*core.SVG {
	logos := map[string]*core.SVG{}
	for key, path := range brandicons.Paths() {
		logos[key] = core.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="` + path + `"/></svg>`))
	}
	logos["chromium"] = logos["chrome"]
	return logos
}()

// Provider identity, rather than display names or model choices, selects the
// artwork. Unrecognized/custom Providers retain a neutral generic Agent icon.
func agentIcon(providerID string) *core.SVG {
	key := ""
	switch providerID {
	case "codex-app", "codex-cli":
		key = "openai"
	case "claude-code":
		key = "claude"
	case "deepseek-harness":
		key = "deepseek"
	case "gemini":
		key = "googlegemini"
	case "cursor", "opencode", "pi":
		key = providerID
	}
	if svg := brandLogos[key]; svg != nil {
		return svg
	}
	return lucide.Bot()
}

func agentGlyph(cx *core.Scope, providerID string, size core.Points) core.Node {
	return glyph(cx, func() *core.SVG { return agentIcon(providerID) }, size)
}

func sessionIcon(session Object) func() *core.SVG {
	return func() *core.SVG { return agentIcon(str(session, "providerId")) }
}
