package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/monkeyWie/reignhub/internal/rpc"
	"github.com/monkeyWie/reignhub/internal/system"
)

func (p *Provider) startAppServer(ctx context.Context) error {
	p.connectMu.Lock()
	defer p.connectMu.Unlock()
	p.mu.Lock()
	b, c := p.ipc, p.client
	p.mu.Unlock()
	if b == nil {
		b = newAppIPC(p)
		p.mu.Lock()
		p.ipc = b
		p.mu.Unlock()
		attempt, stop := context.WithTimeout(ctx, 3*time.Second)
		err := b.connect(attempt)
		stop()
		go b.run()
		if err != nil {
			return fmt.Errorf("连接 Codex App IPC 失败，请先打开官方 App: %w", err)
		}
	}
	ipc := b.client()
	if ipc == nil || !ipc.Alive() {
		return fmt.Errorf("Codex App IPC 尚未就绪，请稍后重试")
	}
	if c != nil && c.Alive() {
		return nil
	}
	app := system.FindApp(p.cfg.CodexApp)
	cli := system.FindCLI(p.cfg.CodexCLI, app)
	if cli == "" {
		return fmt.Errorf("未找到 Codex CLI；请安装官方 App 或设置 codexCli")
	}
	if err := os.MkdirAll(p.cfg.DataDir, 0700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(p.cfg.DataDir, "codex.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	cmd := exec.Command(cli, "-c", "features.code_mode_host=true", "app-server", "--listen", "stdio://")
	cmd.Stderr = logFile
	setup, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	c, err = rpc.StartProcess(setup, cmd, p.observe)
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("启动 Codex 私有后端失败: %w", err)
	}
	b.mu.Lock()
	b.owned = map[string]*ownedThread{}
	b.mu.Unlock()
	p.mu.Lock()
	p.client = c
	p.endpoint = "stdio"
	p.created = map[string]*rpc.Client{}
	p.lastError = ""
	p.mu.Unlock()
	go func() {
		<-c.Done()
		_ = logFile.Close()
		p.mu.Lock()
		current := p.client == c
		p.mu.Unlock()
		if current {
			p.clearRoute("service")
		}
	}()
	return nil
}
