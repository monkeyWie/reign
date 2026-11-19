//go:build !((darwin && !ios) || (linux && !android) || windows || freebsd || openbsd || netbsd || dragonfly)

package codex

const localSupported = false
