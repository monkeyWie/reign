package hubui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type pairingFixture struct {
	*demoAPI
	steps           chan connectivity.Progress
	finish          chan error
	calls, canceled atomic.Int32
}

func (*pairingFixture) CanHost() bool { return false }
func (f *pairingFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method != "client.connect" {
		return f.demoAPI.Call(ctx, method, p, target, r)
	}
	f.calls.Add(1)
	connectivity.Report(ctx, "approval", "请在被控电脑的 ReignHub 中允许此次连接", 125*time.Second)
	for {
		select {
		case <-ctx.Done():
			f.canceled.Add(1)
			return nil, ctx.Err()
		case step := <-f.steps:
			connectivity.Report(ctx, step.Stage, step.Message, step.Timeout)
		case err := <-f.finish:
			if err != nil {
				return nil, err
			}
			return []Object{{"id": "demo-device", "service": str(p, "service"), "connectionId": str(p, "code"), "name": "MacBook Pro", "connected": true, "path": "P2P · WebRTC"}}, nil
		}
	}
}
func startPairing(t *testing.T) (*nsgotest.App, *pairingFixture) {
	t.Helper()
	f := &pairingFixture{demoAPI: newDemoAPI(), steps: make(chan connectivity.Progress, 2), finish: make(chan error, 1)}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	app.WaitFor("id:connect-device", time.Second)
	app.Press("id:connect-device")
	app.Type("id:connection-code", "123456")
	app.Press("id:connect-submit")
	app.WaitFor("id:connection-loading", time.Second)
	app.WaitFor("text:请在被控电脑的 ReignHub 中允许此次连接", time.Second)
	return app, f
}
func TestPairingProgressStaysVisibleAndCanBeCanceled(t *testing.T) {
	app, f := startPairing(t)
	spinner := app.Find("id:connection-loading").ID
	f.steps <- connectivity.Progress{Stage: "direct", Message: "正在尝试 P2P 直连", Timeout: 25 * time.Second}
	app.WaitFor("text:正在尝试 P2P 直连", time.Second)
	// Elapsed updates must not remount the native dialog or restart its spinner.
	app.WaitUntil(3*time.Second, func() bool { return !strings.HasPrefix(app.Find("id:connect-elapsed").Text, "已用 0 秒") })
	if app.Find("id:connection-loading").ID != spinner {
		t.Fatal("progress remounted the dialog")
	}
	app.Screenshot("phone-pairing-progress")
	app.Press("id:connect-cancel")
	app.WaitUntil(time.Second, func() bool { return f.canceled.Load() == 1 })
	if app.Exists("id:dialog-connecting") || app.Exists("id:error-banner") || f.calls.Load() != 1 {
		t.Fatal("cancel retained progress, showed an error, or duplicated the request")
	}
}
func TestPairingFailureSupportsRetryWithoutLosingDevice(t *testing.T) {
	app, f := startPairing(t)
	f.finish <- errors.New("等待主机授权超时")
	app.WaitFor("text:等待授权未完成", time.Second)
	if app.Exists("id:connection-loading") || app.Exists("id:error-banner") {
		t.Fatal("failure is still loading or duplicated as a toast")
	}
	app.Screenshot("phone-pairing-failed")
	app.Press("id:connect-retry")
	app.WaitUntil(time.Second, func() bool { return f.calls.Load() == 2 })
	f.finish <- nil
	app.WaitFor("id:session-sidebar", 3*time.Second)
	if app.Exists("id:dialog-connecting") {
		t.Fatal("success did not dismiss progress")
	}
}
func TestPairingFailureEditKeepsServiceAndDeviceID(t *testing.T) {
	app, f := startPairing(t)
	f.finish <- errors.New("被控设备离线")
	app.WaitFor("text:被控电脑不在线", time.Second)
	app.Press("id:connect-edit")
	app.WaitFor("id:connection-code", time.Second)
	if app.Find("id:connection-code").Text != "123456" || app.Find("id:connection-service").Text != DefaultService {
		t.Fatal("editing discarded the target")
	}
}
