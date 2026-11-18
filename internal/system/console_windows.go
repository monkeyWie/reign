package system

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// HideConsole keeps background probes and stdio Agents from allocating a
// console. SW_HIDE alone can still create a briefly visible console host.
// Preserve the caller's process attributes and its ordinary cancellation rules.
func HideConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
