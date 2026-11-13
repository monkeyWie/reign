package host

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/remote"
	"github.com/monkeyWie/reign/internal/tunnel"
	"github.com/monkeyWie/reign/pkg/link"
)

// startTunnel runs independently: slow provisioning must never delay the
// primary relay, device presence, approvals or Agent access.
func (m *Manager) startTunnel(ctx context.Context, c config.Config, i device.Identity, opts remote.Options) {
	report := func(s tunnel.Status) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if ctx.Err() == nil {
			m.tunnelStatus = s
		}
	}
	for ctx.Err() == nil {
		report(tunnel.Status{State: "connecting", Detail: "正在准备备用云线路"})
		listen, connector, err := tunnelConfig(ctx, c, i)
		if err == nil {
			var h *tunnel.Host
			h, err = tunnel.Listen(ctx, listen, i.ID, opts, m.d, func(token string) (string, bool) {
				m.mu.Lock()
				defer m.mu.Unlock()
				for id, g := range m.state.Grants {
					if g.Expires != 0 && g.Expires <= time.Now().Unix() {
						continue
					}
					expected := link.TunnelToken(g.Key, i.ID, id)
					if subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1 {
						return id, true
					}
				}
				return "", false
			})
			if err == nil {
				defer h.Close()
				m.mu.Lock()
				if ctx.Err() != nil {
					m.mu.Unlock()
					return
				}
				m.tunnel = h
				m.mu.Unlock()
				tunnel.Run(ctx, connector, h.URL, report)
				return
			}
		}
		report(tunnel.Status{State: "error", Detail: "备用云线路暂不可用，继续使用直连或自建中继"})
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}
func tunnelConfig(ctx context.Context, c config.Config, i device.Identity) (string, tunnel.Config, error) {
	connector := tunnel.Config{Binary: c.CloudflaredPath, DataDir: c.DataDir, TokenFile: c.CloudflareTokenFile, PublicURL: c.CloudflarePublicURL}
	listen := c.CloudflareListen
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	if connector.TokenFile != "" {
		if endpoint, err := link.PublicEndpoint(connector.PublicURL); err != nil || endpoint == "" {
			return "", connector, errors.New("named tunnel requires an HTTPS origin")
		}
		return listen, connector, nil
	}
	var result struct{ URL, Token, Listen string }
	attempt, stop := context.WithTimeout(ctx, 12*time.Second)
	defer stop()
	err := device.Post(attempt, i.Service, i.Token, "/cloud/tunnel", map[string]any{}, &result)
	if err != nil {
		var httpErr *device.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 404 {
			return listen, connector, nil
		}
		return "", connector, err
	}
	endpoint, err := link.PublicEndpoint(result.URL)
	if err != nil || endpoint == "" || result.Token == "" || result.Listen != "127.0.0.1:26332" {
		return "", connector, errors.New("invalid managed tunnel configuration")
	}
	connector.TokenFile, err = tunnel.TokenFile(c.DataDir, result.Token)
	connector.PublicURL = endpoint
	return result.Listen, connector, err
}
