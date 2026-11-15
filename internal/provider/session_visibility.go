package provider

import (
	"encoding/json"
	"strings"
)

// SyncSession excludes non-interactive Codex execution histories. The official
// TypeScript SDK launches `codex exec` with originator codex_sdk_ts. Check both
// fields because older records may only retain the execution source.
func SyncSession(s Session) bool {
	if s.ProviderID != "codex-app" && s.ProviderID != "codex-cli" {
		return true
	}
	origin := strings.ToLower(strings.TrimSpace(s.Originator))
	var source string
	_ = json.Unmarshal([]byte(s.Source), &source)
	return source != "exec" && s.SourceKind != "exec" && s.SourceKind != "sdk" &&
		origin != "codex_sdk" && !strings.HasPrefix(origin, "codex_sdk_")
}
