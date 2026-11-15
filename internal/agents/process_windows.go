package agents

import (
	"os/exec"
	"syscall"
)

func isolateProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
