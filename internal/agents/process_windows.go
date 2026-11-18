package agents

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/monkeyWie/reignhub/internal/system"
)

func isolateProcess(cmd *exec.Cmd) { system.HideConsole(cmd) }

func validateExecutable(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".cmd") || strings.EqualFold(filepath.Ext(path), ".bat") {
		return errors.New("请配置 Agent 的 .exe 或使用 node.exe 加脚本路径启动，避免通过 shell 解释参数")
	}
	return nil
}
