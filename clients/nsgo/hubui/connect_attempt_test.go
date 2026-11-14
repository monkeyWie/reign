package hubui

import (
	"context"
	"errors"
	"fmt"
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

type retryProgressFixture struct {
	*demoAPI
	steps  chan connectivity.Progress
	finish chan error
	calls  atomic.Int32
}

func (*retryProgressFixture) CanHost() bool { return false }
func (f *retryProgressFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method != "client.reconnect" {
		return f.demoAPI.Call(ctx, method, p, target, r)
	}
	f.calls.Add(1)
	connectivity.Report(ctx, "signal", "正在建立信令连接", 12*time.Second)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case step := <-f.steps:
			if observe := connectivity.Observer(ctx); observe != nil {
				observe(step)
			}
		case err := <-f.finish:
			if err != nil {
				return nil, err
			}
			return f.demoAPI.Call(ctx, method, p, target, r)
		}
	}
}
func TestRetryKeepsStatusPanelAndShowsActualTransportSteps(t *testing.T) {
	f := &retryProgressFixture{demoAPI: newDemoAPI(), steps: make(chan connectivity.Progress, 8), finish: make(chan error, 1)}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	openDemoChat(t, app)
	app.Type("id:chat-input", "重试时保留草稿")
	app.Press("id:connection-status-demo-device")
	app.Press("id:connection-retry")
	app.WaitFor("text:正在建立信令连接", time.Second)
	if app.Exists("id:dialog-connecting") || !app.Exists("id:dialog-connection-status") {
		t.Fatal("retry opened a separate progress panel")
	}
	spinner := app.Find("id:connection-loading").ID
	f.steps <- connectivity.Progress{ID: "stun:cf", Stage: "udp", Message: "公共 STUN 节点", Detail: "stun:stun.cloudflare.com:3478 · 并行探测", State: "info"}
	f.steps <- connectivity.Progress{ID: "stun:self", Stage: "udp", Message: "自建 STUN 节点", Detail: "stun:probe.example.com:26330 · 并行探测", State: "info"}
	f.steps <- connectivity.Progress{ID: "tcp", Stage: "tcp", Message: "探测 TCP 公网映射", Detail: "probe.example.com:26330", State: "running"}
	f.steps <- connectivity.Progress{ID: "path", Stage: "relay", Message: "正在使用加密中继", Detail: "wss://reignhub.app/p2p/relay", State: "success"}
	app.WaitFor("text:wss://reignhub.app/p2p/relay", time.Second)
	app.Find("text:stun:stun.cloudflare.com:3478 · 并行探测")
	app.Find("text:stun:probe.example.com:26330 · 并行探测")
	if app.Find("id:connection-loading").ID != spinner {
		t.Fatal("progress remounted the status panel")
	}
	app.Screenshot("phone-retry-connection-details")
	f.finish <- errors.New("网络验证失败")
	app.WaitFor("id:connection-retry", time.Second)
	app.Press("id:connection-retry")
	app.WaitUntil(time.Second, func() bool { return f.calls.Load() == 2 })
	f.steps <- connectivity.Progress{ID: "path", Stage: "direct", Message: "已切换为 TCP 直连", Detail: "192.168.1.2:5000 → 192.168.1.3:5000", State: "success"}
	app.WaitFor("text:已切换为 TCP 直连", time.Second)
	f.finish <- nil
	app.WaitFor("text:连接正常，消息会持续同步。", time.Second)
	if app.Exists("id:connection-error-raw") || app.Exists("id:error-banner") {
		t.Fatal("successful retry retained stale failure")
	}
	app.Press("id:connection-done")
	if app.Find("id:chat-input").Text != "重试时保留草稿" {
		t.Fatal("retry discarded draft")
	}
}
func TestConnectionTimelineIsBoundedAndUpdatesNodeResults(t *testing.T) {
	a := &connectAttempt{started: time.Now()}
	a.record(connectivity.Progress{ID: "stun:node", Message: "探测节点", State: "running"})
	at := a.steps[0].at
	a.record(connectivity.Progress{ID: "stun:node", Message: "已获得公网映射", State: "success"})
	if len(a.steps) != 1 || a.steps[0].at != at || a.steps[0].State != "success" {
		t.Fatal("node result was duplicated")
	}
	for i := 0; i < 200; i++ {
		a.record(connectivity.Progress{ID: fmt.Sprint(i), Message: "候选地址", State: "info"})
	}
	if len(a.steps) != 80 {
		t.Fatal("unbounded connection history")
	}
}
