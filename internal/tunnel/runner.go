// Package tunnel supervises an optional cloudflared connector. Agent protocols
// and encryption stay in Reign; cloudflared forwards opaque WebSocket traffic.
package tunnel

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Binary  string
	DataDir string
	// Named tunnels must be configured in Cloudflare to forward PublicURL to the
	// loopback origin. The token stays in a private file, never process arguments.
	TokenFile string
	PublicURL string
}
type Status struct {
	URL    string `json:"url,omitempty"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

var quickURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func resolve(binary string) (string, error) {
	if binary != "" {
		return exec.LookPath(binary)
	}
	if p, e := exec.LookPath("cloudflared"); e == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/cloudflared", "/usr/local/bin/cloudflared"} {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("尚未安装 cloudflared；安装后自动重试，当前仍可直连或使用自建中继")
}

// Run retries failures without affecting the primary device service. A quick
// tunnel is intended for evaluation; production uses a provisioned named tunnel.
func Run(ctx context.Context, c Config, origin string, report func(Status)) {
	for ctx.Err() == nil {
		err := runOnce(ctx, c, origin, report)
		if ctx.Err() != nil {
			break
		}
		detail := "Cloudflare 连接中断，继续使用其它线路"
		if err != nil {
			detail = err.Error()
		}
		report(Status{State: "error", Detail: detail})
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
	}
	report(Status{State: "stopped"})
}
func runOnce(ctx context.Context, c Config, origin string, report func(Status)) error {
	binary, err := resolve(c.Binary)
	if err != nil && c.Binary == "" && c.DataDir != "" {
		report(Status{State: "installing", Detail: "正在下载备用云连接组件"})
		binary, err = Install(ctx, c.DataDir)
	}
	if err != nil {
		return err
	}
	args := []string{"tunnel", "--no-autoupdate", "--protocol", "auto"}
	if c.TokenFile != "" {
		args = append(args, "run", "--token-file", c.TokenFile)
	} else {
		// Ignore any unrelated ~/.cloudflared/config.yml: this connector owns only
		// Reign's loopback origin, never a user's other tunnel configuration.
		args = append(args, "--config", os.DevNull, "--url", origin)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = 3 * time.Second
	var mu sync.Mutex
	endpoint := c.PublicURL
	consume := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		if c.TokenFile == "" {
			if u := quickURL.FindString(line); u != "" {
				endpoint = u
			}
		}
		if strings.Contains(line, "Registered tunnel connection") && endpoint != "" {
			report(Status{State: "connected", URL: endpoint})
		}
	}
	cmd.Stdout = &lineWriter{consume: consume}
	cmd.Stderr = &lineWriter{consume: consume}
	report(Status{State: "connecting"})
	err = cmd.Run()
	return err
}

// TokenFile writes a connector token with owner-only permissions. Provisioning
// belongs to an operator or commercial control plane, not the transport core.
func TokenFile(dir, token string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "cloudflared-token")
	f, e := os.CreateTemp(dir, ".connector-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	_, e = f.WriteString(token)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), p)
	}
	return p, e
}

// os/exec drains these writers before Wait returns. Bounded buffers also avoid
// depending on cloudflared's log line lengths or exposing its raw log contents.
type lineWriter struct {
	pending []byte
	consume func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			if len(w.pending)+len(p) <= 256<<10 {
				w.pending = append(w.pending, p...)
			}
			break
		}
		if len(w.pending)+i <= 256<<10 {
			w.pending = append(w.pending, p[:i]...)
			w.consume(string(w.pending))
		}
		w.pending = w.pending[:0]
		p = p[i+1:]
	}
	return n, nil
}
