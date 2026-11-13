//go:build !windows

package system

import (
	"os/exec"
	"syscall"
)

// IndependentAgentProcess is only for official Agent runtimes that must outlive
// Agent Hub. It is never used to launch a Hub helper.
func IndependentAgentProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
