package codex

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
	"github.com/monkeyWie/reignhub/internal/system"
)

type sharedRuntime struct {
	Endpoint   string              `json:"endpoint"`
	PID        int                 `json:"pid"`
	CLI        string              `json:"cli"`
	Executable string              `json:"executable"`
	StartedAt  string              `json:"startedAt"`
	Desktop    []provider.Instance `json:"desktop,omitempty"`
}

func (p *Provider) runtimeFile() string { return filepath.Join(p.cfg.DataDir, "codex-app-server.json") }

func validRuntimeEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port > 0 && port <= 65535 && u.Scheme == "ws" && u.Hostname() == "127.0.0.1" &&
		(u.Path == "" || u.Path == "/") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func runtimeAlive(ctx context.Context, state sharedRuntime) bool {
	if state.PID <= 0 || !validRuntimeEndpoint(state.Endpoint) {
		return false
	}
	started, executable, err := system.ProcessStamp(ctx, state.PID)
	return err == nil && started == state.StartedAt && executable == state.Executable
}

func (p *Provider) connectRuntime(ctx context.Context, state sharedRuntime) error {
	c, err := rpc.Dial(ctx, state.Endpoint, p.observe)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.client, p.endpoint, p.shared, p.lastError = c, state.Endpoint, state, ""
	p.mu.Unlock()
	go func() { <-c.Done(); p.clearRoute("service") }()
	return nil
}

func (p *Provider) startAppServer(ctx context.Context) error {
	p.connectMu.Lock()
	defer p.connectMu.Unlock()
	p.mu.Lock()
	alive := p.client != nil && p.client.Alive()
	p.mu.Unlock()
	if alive {
		return nil
	}
	app := system.FindApp(p.cfg.CodexApp)
	cli := system.FindCLI(p.cfg.CodexCLI, app)
	if cli == "" {
		return fmt.Errorf("未找到 Codex CLI；请安装官方 App 或设置 codexCli")
	}
	var state sharedRuntime
	loadErr := device.Load(p.runtimeFile(), &state)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		return fmt.Errorf("读取共享 Codex runtime 记录失败：%w", loadErr)
	}
	if loadErr == nil && state.CLI == cli && runtimeAlive(ctx, state) {
		// Never launch another backend behind an App that still uses this one.
		if err := p.connectRuntime(ctx, state); err != nil {
			return fmt.Errorf("官方共享 app-server 仍在运行，但连接失败：%w", err)
		}
		return nil
	}
	listenAddress := "127.0.0.1:0"
	var desktop []provider.Instance
	if loadErr == nil && state.CLI == cli && validRuntimeEndpoint(state.Endpoint) {
		// An already admitted App retains this URL in its launch environment.
		// Restore a stopped backend at the same address so it can reconnect.
		// Never silently select another port behind that still-running App.
		u, _ := url.Parse(state.Endpoint)
		listenAddress = u.Host
		current, err := system.Discover(ctx, app)
		if err != nil && len(state.Desktop) > 0 {
			return fmt.Errorf("验证已有 Codex App 实例失败：%w", err)
		}
		if system.SameInstances(current, state.Desktop) {
			desktop = state.Desktop
		}
	}
	listener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("共享 app-server 地址不可用（%s）；未启动替代后端：%w", listenAddress, err)
	}
	endpoint := "ws://" + listener.Addr().String()
	_ = listener.Close()
	if err := os.MkdirAll(p.cfg.DataDir, 0700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(p.cfg.DataDir, "codex.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	// This is the official Codex runtime, not an Agent Hub helper. Its lifetime
	// deliberately does not inherit Hub's context, request or process session.
	cmd := exec.Command(cli, "-c", "features.code_mode_host=true", "app-server", "--listen", endpoint)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	system.IndependentAgentProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	exited := make(chan error, 1)
	go func() { err := cmd.Wait(); _ = logFile.Close(); exited <- err }()
	// Only an unsuccessful initial launch is ours to clean up. A published,
	// healthy runtime is shared with Codex App and must survive Hub shutdown.
	published := false
	defer func() {
		if !published {
			_ = cmd.Process.Kill()
		}
	}()
	state = sharedRuntime{Endpoint: endpoint, PID: cmd.Process.Pid, CLI: cli, Desktop: desktop}
	state.StartedAt, state.Executable, err = system.ProcessStamp(ctx, state.PID)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		attempt, stop := context.WithTimeout(ctx, time.Second)
		err = p.connectRuntime(attempt, state)
		stop()
		if err == nil {
			if err := device.Save(p.runtimeFile(), state); err != nil {
				_ = p.Close()
				return err
			}
			published = true
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			return fmt.Errorf("Codex app-server 提前退出：%v", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("app-server 启动超时，请查看 %s", logFile.Name())
}

// A manually launched App is not admitted just because it is installed or can
// see the same on-disk sessions. Its process generation must be Hub's recorded
// launch and it must still have a live connection to this exact official server.
func admittedDesktop(current, admitted []provider.Instance, connections int) bool {
	return len(admitted) > 0 && system.SameInstances(current, admitted) && connections > 0
}

func (p *Provider) desktopConnected(ctx context.Context, current []provider.Instance) (int, error) {
	p.mu.Lock()
	state := p.shared
	p.mu.Unlock()
	if !system.SameInstances(current, state.Desktop) || len(state.Desktop) == 0 {
		return 0, nil
	}
	connections, err := system.DesktopConnections(ctx, current, state.Endpoint)
	if err != nil {
		return 0, err
	}
	if !admittedDesktop(current, state.Desktop, connections) {
		return 0, nil
	}
	return connections, nil
}
