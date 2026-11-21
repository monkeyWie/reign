package servercmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/monkeyWie/reignhub/internal/deviceservice"
	"github.com/monkeyWie/reignhub/internal/nativepath"
	"github.com/pion/stun/v3"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c := DefaultConfig()
	c.NativeListen = "127.0.0.1:0"
	c.Listen = "127.0.0.1:0"
	c.STUNListen = "127.0.0.1:0"
	c.TCPSTUNListen = "127.0.0.1:0"
	c.State = filepath.Join(t.TempDir(), "state", "server.db")
	return c
}
func TestConfigFileLifecycle(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd, err := parseCommand(nil, io.Discard)
	if err != nil || cmd.file != "config.json" || cmd.action != serve {
		t.Fatalf("default command: %+v %v", cmd, err)
	}
	if _, err := loadConfig(cmd.file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected missing config", err)
	}
	if err := writeDefaultConfig(cmd.file); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(cmd.file)
	cwd, _ := os.Getwd()
	if err != nil || !c.HTTP || c.State != filepath.Join(cwd, "data", "reignhub.db") {
		t.Fatal(c, err)
	}
	if _, err := os.Stat("data"); !os.IsNotExist(err) {
		t.Fatal("init created state")
	}
	file := filepath.Join(t.TempDir(), "custom.json")
	custom := []byte(`{"http":false,"tcpStun":false,"stunListen":"[::1]:40001","web":"console","tlsCert":"cert","tlsKey":"key"}`)
	if err := os.WriteFile(file, custom, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultConfig(file); err == nil {
		t.Fatal("overwrote existing config")
	}
	got, _ := os.ReadFile(file)
	if !bytes.Equal(custom, got) {
		t.Fatal("changed existing config")
	}
	c, err = loadConfig(file)
	if err != nil || c.HTTP || c.TCPSTUN || !c.STUN || c.STUNListen != "[::1]:40001" || c.Web != filepath.Join(filepath.Dir(file), "console") || c.TLSKey != filepath.Join(filepath.Dir(file), "key") {
		t.Fatal(c, err)
	}
	for _, value := range []string{`{"typo":true}`, `null`, `{}` + " {}", `{"relay":true,"signal":false}`, `{"relayControl":"http://remote.example"}`, `{}` + strings.Repeat(" ", 1<<20)} {
		if err := os.WriteFile(file, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(file); err == nil {
			t.Fatal("accepted invalid config")
		}
	}
	for _, args := range [][]string{{"--only", "stun"}, {"--state", "file"}, {"--init", "--print-relay-pin"}, {"--listen", ":8080"}, {"extra"}} {
		if _, err := parseCommand(args, io.Discard); err == nil {
			t.Fatal("accepted", args)
		}
	}
}
func checkProbes(t *testing.T, r *Runtime) {
	t.Helper()
	c, err := net.Dial("tcp", r.TCPAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	mapped, err := connectivity.TCPBinding(ctx, c)
	if err != nil || mapped.String() != c.LocalAddr().String() {
		t.Fatal(mapped, err)
	}
	udp, err := net.Dial("udp", r.UDPAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(2 * time.Second))
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest, stun.Fingerprint)
	if _, err = udp.Write(request.Raw); err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 1500)
	n, err := udp.Read(raw)
	if err != nil {
		t.Fatal(err)
	}
	response := stun.New()
	response.Raw = raw[:n]
	if err = response.Decode(); err != nil || response.TransactionID != request.TransactionID {
		t.Fatal("invalid UDP binding", err)
	}
	var addr stun.XORMappedAddress
	if err = addr.GetFrom(response); err != nil || net.JoinHostPort(addr.IP.String(), fmtPort(addr.Port)) != udp.LocalAddr().String() {
		t.Fatal(addr, err)
	}
}
func fmtPort(port int) string { return strconv.Itoa(port) }
func TestProbeOnlyDoesNotCreateDatabase(t *testing.T) {
	c := testConfig(t)
	c.HTTP = false
	r, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.HTTPAddr != nil {
		t.Fatal("probe-only opened HTTP")
	}
	if _, err = os.Stat(c.State); !os.IsNotExist(err) {
		t.Fatal("probe-only touched database", err)
	}
	checkProbes(t, r)
}
func TestAllFeaturesRestartAndGracefulSocketShutdown(t *testing.T) {
	c := testConfig(t)
	web := t.TempDir()
	c.Web = web
	for name, content := range map[string]string{"index.html": "<title>ReignHub</title>", "nsgo.json": "{}", "app.0123456789.part-000.bin": "part"} {
		if err := os.WriteFile(filepath.Join(web, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	base := "http://" + r.HTTPAddr.String()
	for _, path := range []string{"/healthz", "/chat/", "/assets/chat/nsgo.json", "/api/account"} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatal(path, res.StatusCode)
		}
	}
	checkProbes(t, r)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	identity, _ := device.NewIdentity("fixture")
	identity, err = device.Register(ctx, base, identity, true)
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := websocket.Dial(ctx, "ws://"+r.HTTPAddr.String()+"/p2p/signal?id="+identity.ID+"&role=host", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + identity.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	if _, _, err = ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	key := r.directory.Network.GrantPublicKey
	sid := r.directory.ServiceID
	done := make(chan struct{})
	go func() { _ = r.Close(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("shutdown blocked on open WebSocket")
	}
	if _, _, err = ws.Read(ctx); err == nil {
		t.Fatal("shutdown left WebSocket open")
	}
	next, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.directory.Network.GrantPublicKey != key || next.directory.ServiceID != sid {
		t.Fatal("restart rotated service identity")
	}
	if _, ok := next.directory.Authenticate(identity.Token); !ok {
		t.Fatal("restart lost registration")
	}
}
func TestBindFailureRollsBackOtherListeners(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := testConfig(t)
	c.Listen = reserve.Addr().String()
	_ = reserve.Close()
	c.STUNListen = occupied.LocalAddr().String()
	if _, err = Start(c); err == nil {
		t.Fatal("ignored UDP bind failure")
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		t.Fatal("HTTP leaked after startup failure", err)
	}
	listener.Close()
}
func TestDisabledRoutesAndConfigContract(t *testing.T) {
	c := testConfig(t)
	c.Signal = false
	c.Relay = false
	c.WebEnabled = false
	c.STUN = false
	c.TCPSTUN = false
	r, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	base := "http://" + r.HTTPAddr.String()
	res, err := http.Post(base+"/p2p/config", "application/json", bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var cfg map[string]any
	if err = json.NewDecoder(res.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["protocol"] != float64(1) || cfg["accountRequired"] != false || len(cfg["loginProviders"].([]any)) != 0 || len(cfg["selfStun"].([]any)) != 0 {
		t.Fatal(cfg)
	}
	for _, path := range []string{"/chat/", "/p2p/signal", "/p2p/relay"} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatal("disabled feature exposed", path, res.StatusCode)
		}
	}
}

func TestConfiguredListenersAndRoutes(t *testing.T) {
	type expected struct{ api, web, signal, relay, udp, tcp bool }
	tests := []struct {
		name string
		want expected
	}{
		{"all", expected{true, true, true, true, true, true}},
		{"dual STUN", expected{udp: true, tcp: true}},
		{"UDP only", expected{udp: true}},
		{"TCP only", expected{tcp: true}},
		{"static web", expected{web: true}},
		{"API and probe", expected{api: true, tcp: true}},
		{"signaling", expected{api: true, signal: true}},
		{"relays", expected{api: true, signal: true, relay: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := testConfig(t)
			web := t.TempDir()
			for name, data := range map[string]string{"index.html": "fixture console", "nsgo.json": "{}"} {
				if err := os.WriteFile(filepath.Join(web, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			base.HTTP = tc.want.api || tc.want.web
			base.API, base.WebEnabled, base.Web = tc.want.api, tc.want.web, web
			base.Signal, base.Relay = tc.want.signal, tc.want.relay
			base.STUN, base.TCPSTUN = tc.want.udp, tc.want.tcp
			file := filepath.Join(t.TempDir(), "config.json")
			raw, err := json.Marshal(base)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, raw, 0600); err != nil {
				t.Fatal(err)
			}
			c, err := loadConfig(file)
			if err != nil {
				t.Fatal(err)
			}
			r, err := Start(c)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if (r.UDPAddr != nil) != tc.want.udp || (r.TCPAddr != nil) != tc.want.tcp || (r.native != nil) != tc.want.relay || (r.HTTPAddr != nil) != (tc.want.api || tc.want.web) {
				t.Fatal("unexpected listeners", r.UDPAddr, r.TCPAddr, r.HTTPAddr)
			}
			_, err = os.Stat(c.State)
			if (tc.want.api && err != nil) || (!tc.want.api && !os.IsNotExist(err)) {
				t.Fatal("unexpected SQLite state", err)
			}
			if r.HTTPAddr == nil {
				return
			}
			for path, available := range map[string]bool{"/healthz": true, "/chat/": tc.want.web, "/api/account": tc.want.api, "/p2p/config": tc.want.api, "/p2p/signal": tc.want.signal, "/p2p/relay": tc.want.relay} {
				resp, err := http.Get("http://" + r.HTTPAddr.String() + path)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				// Enabled socket routes reject HTTP requests without a device credential.
				if (resp.StatusCode != http.StatusNotFound) != available {
					t.Errorf("%s: %d", path, resp.StatusCode)
				}
			}
		})
	}
}

func TestPrintRelayPinMatchesRunningAndStoppedRelay(t *testing.T) {
	c := testConfig(t)
	if _, err := deviceservice.ReadRelayPin(c.State); err == nil {
		t.Fatal("missing state returned a pin")
	}
	if _, err := os.Stat(filepath.Dir(c.State)); !os.IsNotExist(err) {
		t.Fatal("pin lookup created state", err)
	}

	r, err := Start(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	pin, err := deviceservice.ReadRelayPin(c.State)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dialer := tls.Dialer{Config: nativepath.ClientTLS(pin)}
	conn, err := dialer.DialContext(ctx, "tcp", r.native.Addr().String())
	if err != nil {
		t.Fatal("printed pin did not authenticate the running relay", err)
	}
	conn.Close()
	r.Close()

	offline, err := deviceservice.ReadRelayPin(c.State)
	if err != nil || offline != pin {
		t.Fatal("could not read the same public pin while stopped", err)
	}
}
