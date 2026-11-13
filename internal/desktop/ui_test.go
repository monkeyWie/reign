package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/monkeyWie/reign/internal/config"
	"github.com/monkeyWie/reign/internal/control"
	"github.com/monkeyWie/reign/internal/provider"
)

func TestHostViewsAndTakeoverConfirmation(t *testing.T) {
	a := &window{cfg: config.Config{RelayURL: "wss://relay.example.com", RelayToken: strings.Repeat("r", 32), HostID: strings.Repeat("a", 32)}, key: strings.Repeat("ab", 32), providerID: "codex", refreshing: true, providers: []provider.Status{{ID: "codex", Name: "Codex Desktop", State: "detected", Installed: true, Running: true, InstallPath: "/Applications/Codex.app", Detail: "发现独立启动的 App；接入需正常退出后重新打开", Capabilities: provider.Capabilities{Desktop: true}}}, network: control.NetworkStatus{State: "connected", Peers: []control.PeerStatus{{ID: "example-client", Path: "P2P · WebRTC"}}}}
	a.updatePairing()
	if a.qr == nil {
		t.Fatal(a.error)
	}
	tt := ui.NewTester(a.view, 1040, 800)
	if !tt.HasText("本机 Agent") || !tt.HasText("Codex Desktop") {
		t.Fatal("host inventory missing")
	}
	for _, label := range []string{"发送指令", "创建会话", "连接远程主机", "工作台"} {
		if tt.HasText(label) {
			t.Fatalf("host exposes conversation UI: %s", label)
		}
	}
	capture(t, tt, "agents.png")
	if err := tt.Click("接入官方 App"); err != nil {
		t.Fatal(err)
	}
	if !a.confirm || !tt.HasText("确认接入") {
		t.Fatal("explicit restart confirmation missing")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.confirm {
		t.Fatal("confirmation not canceled")
	}
	if err := tt.Click("扫码配对"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("复制配对码") {
		t.Fatal("pairing missing")
	}
	capture(t, tt, "pairing.png")
	if err := tt.Click("复制配对码"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != a.pairing {
		t.Fatal("QR and copied pairing payload differ")
	}
	if err := tt.Click("服务设置"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("保存服务设置") {
		t.Fatal("server settings missing")
	}
	capture(t, tt, "connections.png")
	tt.SetSize(850, 650)
	tt.Frame()
	if !tt.HasText("保存服务设置") {
		t.Fatal("settings inaccessible on small window")
	}
}
func capture(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("AGENTHUB_SCREENSHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
