package agents

import (
	"errors"
	"regexp"
	"strings"

	"github.com/monkeyWie/reignhub/internal/config"
)

// Presets describe actual stdio entry points, not aliases for an unrelated API.
// Users install and authenticate each Agent locally before enabling it.
func Catalog(overrides map[string]config.Agent) map[string]config.Agent {
	out := map[string]config.Agent{
		"claude-code":      {Name: "Claude Code", Protocol: "acp", Command: "claude-agent-acp", Env: map[string]string{"CLAUDE_AGENT_ACP_EXPERIMENTAL_V2": "0"}},
		"pi":               {Name: "Pi", Protocol: "pi", Command: "pi"},
		"deepseek-harness": {Name: "DeepSeek Harness", Protocol: "acp", Command: "dsh", Args: []string{"--profile", "acp"}},
		"cursor":           {Name: "Cursor", Protocol: "acp", Command: "cursor-agent", Args: []string{"acp"}},
		"opencode":         {Name: "OpenCode", Protocol: "acp", Command: "opencode", Args: []string{"acp"}},
		"grok-build":       {Name: "Grok Build", Protocol: "acp", Command: "grok", Args: []string{"--permission-mode", "default", "agent", "stdio"}},
		"antigravity":      {Name: "Antigravity", Protocol: "acp", Command: "agy", Args: []string{"acp"}},
		"kiro":             {Name: "Kiro", Protocol: "acp", Command: "kiro-cli", Args: []string{"acp", "--agent-engine=v3", "--auth-method=cli"}},
		"muse-code":        {Name: "Muse Code", Protocol: "acp", Command: "muse", Args: []string{"acp"}, Env: map[string]string{"MUSE_EXPERIMENTAL_ACP": "on"}},
		"fx":               {Name: "fx", Protocol: "acp", Command: "fx", Args: []string{"acp"}},
		"gemini":           {Name: "Gemini CLI", Protocol: "acp", Command: "gemini", Args: []string{"--acp"}},
	}
	for id, def := range overrides {
		out[id] = def
	}
	return out
}

var validID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func Validate(id string, d config.Agent) error {
	if !validID.MatchString(id) || id == "codex-app" || id == "codex-cli" {
		return errors.New("Agent ID 需为小写字母、数字和短横线，且不能使用 Codex 保留 ID")
	}
	if d.Protocol != "acp" && d.Protocol != "pi" {
		return errors.New("请选择 acp 或 pi 协议")
	}
	if strings.TrimSpace(d.Command) == "" || strings.ContainsRune(d.Command, 0) {
		return errors.New("请填写可执行程序路径")
	}
	if len(d.Args) > 128 || len(d.Env) > 128 {
		return errors.New("Agent 启动配置过大")
	}
	for _, v := range d.Args {
		if strings.ContainsRune(v, 0) {
			return errors.New("参数包含无效字符")
		}
	}
	for k, v := range d.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return errors.New("环境变量无效")
		}
	}
	if d.Protocol == "pi" {
		for _, a := range d.Args {
			if a == "--mode" || strings.HasPrefix(a, "--mode=") || a == "--session" || strings.HasPrefix(a, "--session=") || a == "--no-session" || a == "--continue" || a == "-c" || a == "--resume" || a == "-r" || a == "--no-extensions" {
				return errors.New("Pi 的运行模式、会话和审批扩展由 ReignHub 管理")
			}
		}
	}
	return nil
}
