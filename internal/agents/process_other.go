//go:build !unix && !windows

package agents

import "os/exec"

func isolateProcess(*exec.Cmd) {}

func validateExecutable(string) error { return nil }
