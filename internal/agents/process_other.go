//go:build !unix && !windows

package agents

import "os/exec"

func isolateProcess(*exec.Cmd) {}
