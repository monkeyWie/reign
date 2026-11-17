package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
	"os/exec"
	"time"
)

type cliDaemonInfo struct {
	Status, SocketPath, AppServerVersion string
}

// Discovery only asks the CLI for daemon metadata. It does not start the daemon,
// initialize an app-server connection, resume a thread, or enable remote access.
func readCLIDaemon(ctx context.Context, path string) (cliDaemonInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var info cliDaemonInfo
	b, err := exec.CommandContext(ctx, path, "app-server", "daemon", "version").Output()
	if err != nil {
		return info, err
	}
	if err = json.Unmarshal(b, &info); err != nil {
		return info, err
	}
	return info, nil
}
func (p *Provider) discoverCLI(ctx context.Context, path string) (cliDaemonInfo, error) {
	p.probeMu.Lock()
	defer p.probeMu.Unlock()
	if time.Since(p.probeAt) < 5*time.Second {
		return p.probeInfo, p.probeErr
	}
	p.probeInfo, p.probeErr = readCLIDaemon(ctx, path)
	p.probeAt = time.Now()
	return p.probeInfo, p.probeErr
}

// AutoConnect only attaches an already running shared daemon. In particular,
// seeing an installed CLI never starts a new daemon or resumes a session.
func (p *Provider) AutoConnect(ctx context.Context) error {
	if p.kind != "cli" {
		if !p.cfg.ManagedAgents["codex-app"] && p.appBridge() == nil {
			return nil
		}
		return p.Start(ctx)
	}
	s := p.cliStatus(ctx)
	if s.Ready() || !s.Running {
		return nil
	}
	err := p.startCLI(ctx, false)
	if err != nil {
		p.mu.Lock()
		p.lastError = err.Error()
		p.mu.Unlock()
	}
	return err
}
func (p *Provider) startCLI(ctx context.Context, launch bool) error {
	p.connectMu.Lock()
	defer p.connectMu.Unlock()
	p.mu.Lock()
	alive := p.client != nil && p.client.Alive()
	p.mu.Unlock()
	if alive {
		return nil
	}
	path := p.cfg.CodexCLI
	if path == "" {
		path, _ = exec.LookPath("codex")
	}
	if path == "" {
		return fmt.Errorf("未找到 Codex CLI")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	v, e := readCLIDaemon(ctx, path)
	if e != nil {
		return fmt.Errorf("无法发现 CLI 后台；请先在终端启动 codex：%w", e)
	}
	if v.Status != "running" || v.SocketPath == "" {
		if !launch {
			return fmt.Errorf("CLI 共享后台未运行")
		}
		if err := exec.CommandContext(ctx, path, "app-server", "daemon", "start").Run(); err != nil {
			return fmt.Errorf("启动 CLI 共享后台失败：%w", err)
		}
		v, e = readCLIDaemon(ctx, path)
		if e != nil || v.Status != "running" || v.SocketPath == "" {
			return fmt.Errorf("CLI 共享后台启动后尚未就绪")
		}
	}
	p.probeMu.Lock()
	p.probeInfo, p.probeErr, p.probeAt = v, nil, time.Now()
	p.probeMu.Unlock()
	endpoint := "unix://" + v.SocketPath
	c, e := rpc.Dial(ctx, endpoint, p.observe)
	if e != nil {
		return fmt.Errorf("连接 CLI 后台失败：%w", e)
	}
	p.mu.Lock()
	p.client = c
	p.endpoint = endpoint
	p.lastError = ""
	p.mu.Unlock()
	go func() { <-c.Done(); p.clearRoute("service") }()
	return nil
}
func (p *Provider) cliStatus(ctx context.Context) provider.Status {
	path := p.cfg.CodexCLI
	if path == "" {
		path, _ = exec.LookPath("codex")
	}
	p.mu.Lock()
	alive := p.client != nil && p.client.Alive()
	endpoint, lastError := p.endpoint, p.lastError
	p.mu.Unlock()
	s := provider.Status{ID: "codex-cli", Name: "Codex CLI", State: "unavailable", Installed: path != "", InstallPath: path, Instances: []provider.Instance{}, LastError: lastError, Capabilities: provider.Capabilities{Streaming: true, History: true, Create: true, Send: true, Steer: true, Interrupt: true, Approvals: true, ImageInput: true, FileInput: true}}
	if path == "" {
		s.Detail = "未安装 Codex CLI"
		return s
	}
	if alive {
		s.State = "ready"
		s.Running = true
		s.Detail = "已就绪，可以远控"
		s.Endpoint = endpoint
		p.probeMu.Lock()
		s.Version = p.probeInfo.AppServerVersion
		p.probeMu.Unlock()
		return s
	}
	info, err := p.discoverCLI(ctx, path)
	if err != nil {
		s.Detail = "后台状态检测失败，尚未接入"
		s.LastError = err.Error()
		return s
	}
	s.Version = info.AppServerVersion
	if info.Status == "running" && info.SocketPath != "" {
		s.State = "detected"
		s.Running = true
		s.Endpoint = "unix://" + info.SocketPath
		s.Detail = "后台运行中，正在接入"
		if lastError != "" {
			s.Detail = "后台运行中，接入失败：" + lastError
		}
	} else {
		s.Detail = "官方共享后台未运行"
	}
	return s
}

func (p *Provider) session(t thread) provider.Session {
	s := t.session()
	s.Project = p.sessionProject(t)
	s.ProviderID = "codex-app"
	if p.kind == "cli" {
		s.ProviderID = "codex-cli"
	}
	return s
}
