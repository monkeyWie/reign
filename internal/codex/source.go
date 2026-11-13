package codex

import (
	"encoding/json"
	"strings"
)

// The App and IDE have both used source="vscode". Only a recognized recorded
// originator disambiguates them. This is creation provenance, not live ownership.
func (t thread) sourceKind() string {
	var source string
	var structured map[string]json.RawMessage
	_ = json.Unmarshal(t.Source, &source)
	_ = json.Unmarshal(t.Source, &structured)
	if _, ok := structured["subAgent"]; ok {
		return "subAgent"
	}
	if _, ok := structured["subagent"]; ok {
		return "subAgent"
	}
	if strings.HasPrefix(source, "subAgent") {
		return "subAgent"
	}
	if source == "exec" {
		return "exec"
	}
	switch strings.ToLower(strings.TrimSpace(t.Originator)) {
	case "codex desktop", "codex_desktop":
		return "app"
	case "codex_vscode":
		return "vscode"
	case "codex-tui", "codex_cli_rs":
		return "cli"
	case "agenthub":
		return "agenthub"
	}
	switch source {
	case "cli", "exec", "appServer":
		return source
	case "vscode":
		if strings.TrimSpace(t.Originator) != "" {
			return "custom"
		}
		return "ideUnknown"
	}
	if _, ok := structured["custom"]; ok {
		return "custom"
	}
	return "unknown"
}
