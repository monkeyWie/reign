//go:build !windows

package system

import "os/exec"

func HideConsole(*exec.Cmd) {}
