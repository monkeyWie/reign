//go:build unix

package agents

import (
	"os/exec"
	"syscall"
)

func isolateProcess(cmd *exec.Cmd) {
	// Only subprocesses created by this adapter enter this group. A shutdown must
	// never signal a user's independently running terminal or desktop Agent.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func validateExecutable(string) error { return nil }
