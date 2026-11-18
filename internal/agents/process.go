package agents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/internal/config"
)

func isMissing(err error) bool { return errors.Is(err, sql.ErrNoRows) }
func resolveExecutable(command string) (string, error) {
	if command == "" {
		return "", errors.New("请配置 Agent 可执行程序")
	}
	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}
	if !strings.ContainsAny(command, "/\\") {
		home, _ := os.UserHomeDir()
		for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".npm-global", "bin"), filepath.Join(home, ".bun", "bin"), "/opt/homebrew/bin", "/usr/local/bin"} {
			if path, err := exec.LookPath(filepath.Join(dir, command)); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("未找到可执行程序 %s", command)
}

func resolveAgentExecutable(def config.Agent) (string, error) {
	if search, ok := def.Env["PATH"]; ok && !strings.ContainsAny(def.Command, "/\\") {
		for _, dir := range filepath.SplitList(search) {
			if dir == "" {
				continue
			}
			if path, err := exec.LookPath(filepath.Join(dir, def.Command)); err == nil {
				return path, nil
			}
		}
		return "", fmt.Errorf("在配置的 PATH 中未找到 %s", def.Command)
	}
	return resolveExecutable(def.Command)
}

type process struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    io.ReadCloser
	done   chan struct{}
	cancel context.CancelFunc
	once   sync.Once
}

func spawn(parent context.Context, def config.Agent, extra []string, cwd string) (*process, error) {
	path, err := resolveAgentExecutable(def)
	if err != nil {
		return nil, err
	}
	if err := validateExecutable(path); err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(life, path, append(append([]string{}, def.Args...), extra...)...)
	isolateProcess(cmd)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for key, value := range def.Env {
		if strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			cancel()
			return nil, errors.New("Agent 环境配置无效")
		}
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	// stderr never shares the JSON-RPC channel and is not retained without a
	// bound or exposed through the provider status (it can contain credentials).
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, writer, err := os.Pipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, err
	}
	cmd.Stdout = writer
	if err = cmd.Start(); err != nil {
		writer.Close()
		in.Close()
		out.Close()
		cancel()
		return nil, err
	}
	p := &process{cmd: cmd, in: in, out: out, cancel: cancel, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); writer.Close(); close(p.done) }()
	return p, nil
}
func (p *process) Close() error {
	p.once.Do(func() {
		p.in.Close()
		select {
		case <-p.done:
		case <-time.After(500 * time.Millisecond):
			p.cancel()
			<-p.done
		}
		p.cancel()
		p.out.Close()
	})
	return nil
}
func (p *Provider) newRuntime(ctx context.Context, id string) (runtimeAgent, error) {
	switch p.def.Protocol {
	case "acp":
		return newACP(ctx, p, id)
	case "pi":
		return newPi(ctx, p, id)
	default:
		return nil, fmt.Errorf("不支持的 Agent 协议 %q", p.def.Protocol)
	}
}
