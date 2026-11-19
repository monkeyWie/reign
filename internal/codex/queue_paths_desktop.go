//go:build (darwin && !ios) || windows || (linux && !android)

package codex

import (
	"os"
	"path/filepath"
)

// VS Code's default globalStorageUri lives under the platform configuration
// directory. Do not scan profiles or guess a remote/custom user-data directory.
func vscodeQueueFiles() []string {
	root, err := os.UserConfigDir()
	if err != nil || root == "" {
		return nil
	}
	files := make([]string, 0, 2)
	for _, edition := range []string{"Code", "Code - Insiders"} {
		files = append(files, filepath.Join(root, edition, "User", "globalStorage", "openai.chatgpt", "queued-follow-ups.json"))
	}
	return files
}
