// Package host owns local device authorization and Agent lifecycle, outside the UI.
package host

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/device"
	"github.com/monkeyWie/reign/internal/deviceservice"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/monkeyWie/reign/internal/remote"
	"github.com/monkeyWie/reign/internal/store"
	"github.com/monkeyWie/reign/internal/tunnel"
	"golang.org/x/crypto/bcrypt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Grant struct {
	Client  string `json:"client"`
	Name    string `json:"name"`
	Public  string `json:"public"`
	Key     string `json:"key"`
	Expires int64  `json:"expires,omitempty"`
}
type saved struct {
	Identity     device.Identity  `json:"identity"`
	PasswordHash string           `json:"passwordHash,omitempty"`
	Grants       map[string]Grant `json:"grants"`
}
type Intent struct {
	Password string `json:"password"`
	Remember bool   `json:"remember"`
}
type Manager struct {
	mu           sync.Mutex
	activation   sync.Mutex
	cfg          config.Config
	state        saved
	pending      map[string]deviceservice.Pair
	intents      map[string]Intent
	status       string
	tunnel       *tunnel.Host
	tunnelStatus tunnel.Status
	network      *remote.HostStatus
	d            *control.Dispatcher
	ice          func(config.Config) remote.Options
	path         string
	attempts     map[string][]time.Time
}

func New(c config.Config, d *control.Dispatcher, ice func(config.Config) remote.Options) *Manager {
	return &Manager{cfg: c, d: d, ice: ice, pending: map[string]deviceservice.Pair{}, intents: map[string]Intent{}, attempts: map[string][]time.Time{}, status: "尚未配置设备服务"}
}
func (m *Manager) save() error { return saveIdentity(m.path, m.state) }
func saveIdentity(path string, s saved) error {
	db, err := store.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	return db.Put("host:"+device.Fingerprint(s.Identity.Service), s)
}
func (m *Manager) Network() control.NetworkStatus {
	m.mu.Lock()
	n := m.network
	h := m.tunnel
	m.mu.Unlock()
	out := n.Snapshot()
	if h != nil {
		out.Peers = append(out.Peers, h.Status.Snapshot().Peers...)
	}
	return out
}

// Reconcile attachable Agents independently of device registration and pairing.
// This never launches an Agent; disruptive setup remains a local user action.
func (m *Manager) autoConnect(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		for _, p := range m.d.Providers {
			if connector, ok := p.(provider.AutoConnector); ok {
				attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
				_ = connector.AutoConnect(attempt)
				cancel()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (m *Manager) Run(ctx context.Context) {
	done := make(chan struct{})
	go func() { defer close(done); m.autoConnect(ctx) }()
	defer func() { <-done }()
	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	current := ""
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		m.mu.Lock()
		c := m.cfg
		m.mu.Unlock()
		signature := fmt.Sprintf("%s|%t|%s|%s|%s|%s", c.ServiceURL, c.CloudflareEnabled, c.CloudflaredPath, c.CloudflareTokenFile, c.CloudflarePublicURL, c.CloudflareListen)
		if c.ServiceURL != "" && signature != current {
			if cancel != nil {
				cancel()
				cancel = nil
			}
			current = ""
			m.mu.Lock()
			m.network = nil
			m.state = saved{Grants: map[string]Grant{}}
			m.path = ""
			m.status = "正在登记设备"
			m.mu.Unlock()
			if e := m.register(ctx, c); e != nil {
				m.mu.Lock()
				m.status = e.Error()
				m.mu.Unlock()
			} else {
				current = signature
				m.mu.Lock()
				i := m.state.Identity
				n := remote.NewHostStatus(i.Service)
				m.network = n
				m.status = "在线"
				m.mu.Unlock()
				life, stop := context.WithCancel(ctx)
				cancel = stop
				opts := m.ice(c)
				if !opts.RelayOnly {
					policy := device.Network(life, i.Service)
					opts.ICEServers = policy.Servers()
					opts.TCPSTUN = policy.TCPSTUN
				}
				opts.ResolveKey = m.key
				m.mu.Lock()
				m.tunnel = nil
				m.tunnelStatus = tunnel.Status{State: "disabled"}
				m.mu.Unlock()
				if c.CloudflareEnabled {
					go m.startTunnel(life, c, i, opts)
				}
				go remote.RunHostObserved(life, remote.Pairing{Relay: "ws" + strings.TrimPrefix(i.Service, "http"), Host: i.ID, Key: device.Random(), RelayToken: i.Token}, opts, m.d, n)
			}
		}
		if current != "" {
			m.poll(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (m *Manager) register(ctx context.Context, c config.Config) error {
	service, e := device.ServiceURL(c.ServiceURL)
	if e != nil {
		return e
	}
	path := filepath.Join(c.DataDir, "identity.db")
	s := saved{Grants: map[string]Grant{}}
	db, e := store.Open(path)
	if e != nil {
		return e
	}
	e = db.Get("host:"+device.Fingerprint(service), &s)
	_ = db.Close()
	if errors.Is(e, sql.ErrNoRows) {
		legacy := filepath.Join(c.DataDir, "host-"+device.Fingerprint(service)[:16]+".json")
		if err := device.Load(legacy, &s); err != nil && !os.IsNotExist(err) {
			return err
		}
		if s.Identity.Token != "" {
			if err := saveIdentity(path, s); err != nil {
				return err
			}
		}
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if s.Identity.Token == "" {
		name := c.DeviceName
		if name == "" {
			name, _ = os.Hostname()
		}
		s.Identity, e = device.NewIdentity(name)
		if e != nil {
			return e
		}
		s.Identity, e = device.Register(ctx, service, s.Identity, true)
		if e != nil {
			return e
		}
		if e = saveIdentity(path, s); e != nil {
			return e
		}
	}
	if s.Grants == nil {
		s.Grants = map[string]Grant{}
	}
	m.mu.Lock()
	m.state = s
	m.path = path
	m.pending = map[string]deviceservice.Pair{}
	m.intents = map[string]Intent{}
	m.mu.Unlock()
	return nil
}
func (m *Manager) key(id string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.state.Grants[id]
	return g.Key, ok && (g.Expires == 0 || g.Expires > time.Now().Unix())
}
func (m *Manager) poll(ctx context.Context) {
	m.mu.Lock()
	i := m.state.Identity
	m.mu.Unlock()
	if i.ID == "" {
		return
	}
	if e := device.Post(ctx, i.Service, i.Token, "/v2/presence", map[string]any{"name": i.Name, "tunnelUrl": m.publicTunnel()}, nil); e != nil {
		m.mu.Lock()
		m.status = e.Error()
		m.mu.Unlock()
		return
	}
	var pairs []deviceservice.Pair
	if device.Post(ctx, i.Service, i.Token, "/v2/pair/pending", map[string]any{}, &pairs) != nil {
		return
	}
	active := map[string]bool{}
	for _, p := range pairs {
		active[p.ID] = true
		m.mu.Lock()
		_, seen := m.pending[p.ID]
		m.mu.Unlock()
		if seen {
			continue
		}
		var in Intent
		if e := device.Open(i.Private, p.Public, "agenthub/pair/request/"+p.ID, p.Envelope, &in); e != nil {
			continue
		}
		m.mu.Lock()
		m.pending[p.ID] = p
		m.intents[p.ID] = in
		password := m.state.PasswordHash
		attempts := m.attempts[p.Client]
		now := time.Now()
		recent := []time.Time{}
		for _, t := range attempts {
			if now.Sub(t) < time.Minute {
				recent = append(recent, t)
			}
		}
		m.attempts[p.Client] = recent
		m.mu.Unlock()
		if in.Password == "" {
			m.mu.Lock()
			uiPath := m.cfg.UIPath
			m.mu.Unlock()
			// Bring the local authorization window forward, including when it was closed.
			if runtime.GOOS == "darwin" && strings.HasSuffix(uiPath, ".app") {
				cmd := exec.CommandContext(ctx, "/usr/bin/open", uiPath)
				_ = cmd.Run()
			}
		}
		if in.Password != "" {
			ok := len(recent) < 5 && password != "" && bcrypt.CompareHashAndPassword([]byte(password), []byte(in.Password)) == nil
			m.mu.Lock()
			m.attempts[p.Client] = append(recent, now)
			m.mu.Unlock()
			_ = m.respond(ctx, p.ID, ok)
		}
	}
	m.mu.Lock()
	m.status = "在线"
	for id := range m.pending {
		if !active[id] {
			delete(m.pending, id)
			delete(m.intents, id)
		}
	}
	m.mu.Unlock()
}
func (m *Manager) respond(ctx context.Context, id string, allow bool) error {
	m.mu.Lock()
	p, ok := m.pending[id]
	intent := m.intents[id]
	i := m.state.Identity
	m.mu.Unlock()
	if !ok || p.Expires < time.Now().Unix() {
		return fmt.Errorf("授权请求已过期")
	}
	var reply string
	if allow {
		g := Grant{Client: p.Client, Name: p.Name, Public: p.Public, Key: device.Random()}
		if !intent.Remember {
			g.Expires = time.Now().Add(8 * time.Hour).Unix()
		}
		var e error
		reply, e = device.Seal(i.Private, p.Public, "agenthub/pair/reply/"+p.ID, map[string]any{"key": g.Key, "host": i.ID, "public": i.Public, "expires": g.Expires})
		if e != nil {
			return e
		}
		m.mu.Lock()
		old, had := m.state.Grants[g.Client]
		m.state.Grants[g.Client] = g
		e = m.save()
		if e != nil {
			if had {
				m.state.Grants[g.Client] = old
			} else {
				delete(m.state.Grants, g.Client)
			}
		}
		m.mu.Unlock()
		if e != nil {
			return e
		}
	}
	if e := device.Post(ctx, i.Service, i.Token, "/v2/pair/respond", map[string]any{"id": id, "allow": allow, "reply": reply}, nil); e != nil {
		return e
	}
	m.mu.Lock()
	delete(m.pending, id)
	delete(m.intents, id)
	m.mu.Unlock()
	return nil
}
func (m *Manager) Local(ctx context.Context, r control.Request) (any, error) {
	var in struct {
		ServiceURL, Name, Password, ID, Client, Provider string
		Allow                                            bool
		Remember                                         bool
		ClearPassword                                    bool
		CloudflareEnabled                                *bool
		CloudflaredPath                                  string
	}
	if len(r.Params) > 0 {
		if e := json.Unmarshal(r.Params, &in); e != nil {
			return nil, e
		}
	}
	switch r.Method {
	case "host.info":
		m.mu.Lock()
		defer m.mu.Unlock()
		i := m.state.Identity
		pending := []map[string]any{}
		for _, p := range m.pending {
			pending = append(pending, map[string]any{"id": p.ID, "name": p.Name, "fingerprint": device.Fingerprint(p.Public), "expires": p.Expires, "remember": m.intents[p.ID].Remember})
		}
		grants := []map[string]any{}
		for _, g := range m.state.Grants {
			grants = append(grants, map[string]any{"client": g.Client, "name": g.Name, "fingerprint": device.Fingerprint(g.Public), "expires": g.Expires})
		}
		sort.Slice(pending, func(a, b int) bool { return pending[a]["id"].(string) < pending[b]["id"].(string) })
		return map[string]any{"deviceId": i.ID, "connectionId": i.ConnectionID, "public": i.Public, "name": i.Name, "serviceUrl": m.cfg.ServiceURL, "status": m.status, "cloudflareEnabled": m.cfg.CloudflareEnabled, "cloudflaredPath": m.cfg.CloudflaredPath, "tunnel": m.tunnelStatus, "passwordEnabled": m.state.PasswordHash != "", "pending": pending, "grants": grants, "qr": map[string]any{"version": 2, "service": i.Service, "connectionId": i.ConnectionID, "public": i.Public}}, nil
	case "host.configure":
		url, e := device.ServiceURL(in.ServiceURL)
		if e != nil {
			return nil, e
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.cfg.ServiceURL = url
		m.cfg.DeviceName = in.Name
		if in.CloudflareEnabled != nil {
			m.cfg.CloudflareEnabled = *in.CloudflareEnabled
		}
		if in.CloudflaredPath != "" {
			m.cfg.CloudflaredPath = in.CloudflaredPath
		}
		if m.path != "" && m.state.Identity.Service == url && in.Name != "" {
			m.state.Identity.Name = in.Name
			if e := m.save(); e != nil {
				return nil, e
			}
		}
		return nil, m.cfg.Save()
	case "host.password":
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.path == "" {
			return nil, fmt.Errorf("请先连接设备服务")
		}
		if in.ClearPassword {
			m.state.PasswordHash = ""
		} else {
			if len(in.Password) < 8 || len(in.Password) > 72 {
				return nil, fmt.Errorf("密码应为 8–72 字节")
			}
			h, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
			if e != nil {
				return nil, e
			}
			m.state.PasswordHash = string(h)
		}
		return nil, m.save()
	case "host.respond":
		return nil, m.respond(ctx, in.ID, in.Allow)
	case "host.revoke":
		m.mu.Lock()
		i := m.state.Identity
		network := m.network
		tunnelHost := m.tunnel
		delete(m.state.Grants, in.Client)
		e := m.save()
		m.mu.Unlock()
		if e != nil {
			return nil, e
		}
		network.DisconnectDevice(in.Client)
		if tunnelHost != nil {
			tunnelHost.Relay.DisconnectDevice(i.ID, in.Client)
			tunnelHost.Status.DisconnectDevice(in.Client)
		}
		return nil, device.Post(ctx, i.Service, i.Token, "/v2/revoke", map[string]string{"client": in.Client}, nil)
	case "host.enable", "host.activate":
		m.activation.Lock()
		defer m.activation.Unlock()
		p := m.d.Providers[in.Provider]
		if p == nil {
			return nil, fmt.Errorf("未知 Agent")
		}
		if e := p.Start(ctx); e != nil {
			return nil, e
		}
		// Remember backend setup for providers that own a managed backend.
		// CLI attachment is discovered automatically and needs no saved switch.
		if p.Status(ctx).Capabilities.Desktop {
			m.mu.Lock()
			if m.cfg.ManagedAgents == nil {
				m.cfg.ManagedAgents = map[string]bool{}
			}
			m.cfg.ManagedAgents[in.Provider] = true
			e := m.cfg.Save()
			m.mu.Unlock()
			if e != nil {
				return nil, e
			}
		}
		if r.Method == "host.activate" {
			return nil, p.ConnectDesktop(ctx, r.Expected, r.ConfirmRestart)
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown host method")
	}
}

func (m *Manager) publicTunnel() string { m.mu.Lock(); defer m.mu.Unlock(); return m.tunnelStatus.URL }
