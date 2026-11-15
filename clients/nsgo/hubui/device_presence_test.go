package hubui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type directoryFixture struct {
	*pairingFixture
	reads  atomic.Int32
	online atomic.Bool
	broken atomic.Bool
}

func (f *directoryFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	switch method {
	case "client.list":
		return []Object{{"id": "demo-device", "name": "工作电脑", "connectionId": "123456", "service": DefaultService, "connected": false}}, nil
	case "client.presence":
		f.reads.Add(1)
		if f.broken.Load() {
			return nil, errors.New("service unavailable")
		}
		return []Object{{"id": "demo-device", "online": f.online.Load(), "checkedAt": time.Now().Unix()}}, nil
	default:
		return f.pairingFixture.Call(ctx, method, p, target, r)
	}
}

func TestDeviceListShowsServerPresenceWithoutConnecting(t *testing.T) {
	f := &directoryFixture{pairingFixture: &pairingFixture{demoAPI: newDemoAPI(), steps: make(chan connectivity.Progress, 2), finish: make(chan error, 1)}}
	f.online.Store(true)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return RootWithAPI(cx, f, true)
	}, nsgotest.Size(390, 844))
	app.WaitFor("text:在线 · 点击连接", 2*time.Second)
	if f.calls.Load() != 0 || f.reads.Load() != 1 {
		t.Fatal("presence started a connection or duplicated its batch")
	}
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-sidebar", time.Second)
	app.WaitFor("id:session-demo-chat", time.Second)
	if app.Exists("id:dialog-connecting") {
		t.Fatal("saved device navigation blocked on connection")
	}
	app.Press("id:connection-status-demo-device")
	app.WaitFor("text:请在被控电脑的 ReignHub 中允许此次连接", time.Second)
	f.finish <- errors.New("被控设备离线")
	app.WaitFor("text:被控电脑不在线", time.Second)
	app.Press("id:connection-done")
	if !app.Exists("id:session-sidebar") || f.calls.Load() != 1 {
		t.Fatal("failed connection discarded cached page or retried")
	}
	app.Screenshot("device-list-server-presence")
}

func TestPresenceTextDoesNotConfuseOfflineWithDisconnected(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := newState(cx, newDemoAPI())
		for _, test := range []struct {
			online  any
			checked int64
			want    string
		}{
			{true, time.Now().Unix(), "在线 · 点击连接"},
			{false, time.Now().Unix(), "离线"},
			{nil, time.Now().Unix(), "在线状态暂不可用"},
			{true, time.Now().Add(-2 * time.Minute).Unix(), "在线状态暂不可用"},
		} {
			s.devicePresence = map[string]Object{"host": {"online": test.online, "checkedAt": test.checked}}
			if got := s.deviceListStatus(Object{"id": "host"}); got != test.want {
				t.Errorf("presence=%v: %q want %q", test.online, got, test.want)
			}
		}
		return core.View{}
	})
	_ = app
}
