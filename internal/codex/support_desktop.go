//go:build (darwin && !ios) || (linux && !android) || windows || freebsd || openbsd || netbsd || dragonfly

package codex

// Local IPC and a Codex executable do not require desktop App discovery.
const localSupported = true
