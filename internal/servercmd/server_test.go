package servercmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/monkeyWie/reignhub/internal/device"
	"github.com/pion/stun/v3"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	c := DefaultConfig()
	c.Listen = "127.0.0.1:0"
	c.STUNListen = "127.0.0.1:0"
	c.TCPSTUNListen = "127.0.0.1:0"
	c.State = filepath.Join(t.TempDir(), "state", "server.db")
	return c
}
func TestConfigPrecedenceAndValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(file, []byte(`{"http":false,"stunListen":"127.0.0.1:40001","tcpStunListen":"127.0.0.1:40002"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, _, err := ParseConfig([]string{"--config", file, "--stun-listen=127.0.0.1:0", "--tcp-stun=false"}, io.Discard)
	if err != nil || c.HTTP || !c.STUN || c.TCPSTUN || c.STUNListen != "127.0.0.1:0" || c.TCPSTUNListen != "127.0.0.1:40002" {
		t.Fatal(c, err)
	}
	for _, args := range [][]string{{"--http=false", "--stun=false", "--tcp-stun=false"}, {"--signal=false"}, {"--tls-cert=missing"}, {"--origins=https://example.com/path"}, {"extra"}} {
		if _, _, err := ParseConfig(args, io.Discard); err == nil {
			t.Fatalf("accepted invalid config %v", args)
		}
	}
	if err := os.WriteFile(file, []byte(`{"typo":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseConfig([]string{"--config", file}, io.Discard); err == nil {
		t.Fatal("ignored config typo")
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
