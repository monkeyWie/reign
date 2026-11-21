package host

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/control"
)

func TestTerminalPermissionIsLocalAndPersistent(t *testing.T) {
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := &control.Dispatcher{}
	m := New(cfg, d, nil)
	if d.RemoteTerminalAllowed() {
		t.Fatal("remote shell permission defaults on")
	}
	r := control.Request{Method: "host.terminal.enabled", Params: json.RawMessage(`{"enabled":true}`)}
	if out := d.HandleRemote(context.Background(), r); out.Error == "" || d.RemoteTerminalAllowed() {
		t.Fatal("peer granted itself shell access")
	}
	if _, err := m.Local(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(cfg.DataDir)
	if err != nil || !saved.RemoteTerminalEnabled || !d.RemoteTerminalAllowed() {
		t.Fatal("permission not persisted", err)
	}
	r.Params = json.RawMessage(`{"enabled":false}`)
	if _, err := m.Local(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	saved, err = config.Load(cfg.DataDir)
	if err != nil || saved.RemoteTerminalEnabled || d.RemoteTerminalAllowed() {
		t.Fatal("permission not revoked", err)
	}
}
