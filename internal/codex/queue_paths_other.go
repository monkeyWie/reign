//go:build (!darwin && !windows && !linux) || ios || android

package codex

func vscodeQueueFiles() []string { return nil }
