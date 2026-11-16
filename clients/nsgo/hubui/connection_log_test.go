package hubui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/connectivity"
)

func TestConnectionDiagnosticsKeepTransitionsAndRedactSecrets(t *testing.T) {
	s := &State{connections: map[string]*connectionHealth{}, devices: []Object{{"id": "test", "connected": true, "service": "https://user:secret@example.test/?token=hidden"}}}
	a := &connectAttempt{params: Object{"password": "do-not-export", "public": "private-pairing-material", "service": "https://example.test/?key=private"}}
	for _, state := range []string{"running", "success", "warning"} {
		a.record(connectivity.Progress{ID: "same", Stage: "relay", Message: state, Detail: "wss://example.test/p2p/relay?token=hidden", State: state})
	}
	if len(a.events) != 3 || len(a.steps) != 1 {
		t.Fatal("lost transitions or duplicated timeline")
	}
	s.observeTransports()
	s.devices[0]["connected"] = false
	s.observeTransports()
	s.observeTransports()
	s.setConnectionIssue("test", "watch", context.DeadlineExceeded)
	report := s.connectionLog("test", a)
	for _, secret := range []string{"hidden", "do-not-export", "private-pairing-material", "?key=private", "user:secret"} {
		if strings.Contains(report, secret) {
			t.Fatalf("report leaked %s", secret)
		}
	}
	if !strings.Contains(report, "传输连接已断开") || !strings.Contains(report, "context deadline exceeded") {
		t.Fatal("report missing lifecycle details")
	}
	if len(s.connection("test").diagnostics) != 3 {
		t.Fatal("transport transitions not deduplicated")
	}
	for _, raw := range []string{"token=hidden", `{"password":"hidden"}`, "Authorization: Bearer hidden", "https://user:hidden@host.test/path?secret=hidden#hidden"} {
		if strings.Contains(safeDiagnostic(raw), "hidden") {
			t.Fatalf("secret leaked: %s", safeDiagnostic(raw))
		}
	}
	for i := 0; i < 500; i++ {
		a.record(connectivity.Progress{Stage: "relay", Message: time.Unix(int64(i), 0).String()})
	}
	if len(a.events) != diagnosticLimit {
		t.Fatal("unbounded diagnostic history")
	}
	b := &connectAttempt{}
	b.inherit(a)
	b.record(connectivity.Progress{Stage: "retry", Message: "retry"})
	if len(b.events) != diagnosticLimit {
		t.Fatal("retry discarded or unbounded history")
	}
}

func TestReconnectDisablesCommandsWithStaleConnectedSnapshot(t *testing.T) {
	f := &retryProgressFixture{demoAPI: newDemoAPI(), steps: make(chan connectivity.Progress, 8), finish: make(chan error, 1)}
	app, s := conversationFixture(t, f, 390, Object{}, true)
	app.Type("id:chat-input", "保留输入")
	app.WaitUntil(time.Second, func() bool { return !app.Find("id:send-message").Props.Disabled })
	app.Post(func() { s.cx.Post(func() { s.reconnectDevice("demo-device") }) })
	app.WaitFor("id:connection-retrying", time.Second)
	if !app.Find("id:send-message").Props.Disabled {
		t.Fatal("stale connected snapshot enabled send while verifying transport")
	}
	f.finish <- nil
	app.WaitUntil(time.Second, func() bool { return !app.Find("id:send-message").Props.Disabled })
	if app.Find("id:chat-input").Text != "保留输入" {
		t.Fatal("reconnect discarded draft")
	}
}

func TestRecoveryBackoffRemainsConnectingAndInitialFailureDoesNot(t *testing.T) {
	s := &State{api: newDemoAPI(), devices: []Object{{"id": "test", "connected": false, "autoReconnect": true}}}
	h := s.connection("test")
	h.established = true
	h.attempt = &connectAttempt{err: "network reset"}
	if s.connectionTitle("test") != "连接中" || s.connectionReady("test") {
		t.Fatal("backoff became unavailable or enabled commands")
	}
	h.established = false
	if s.connectionRecovering("test") {
		t.Fatal("first connection failure claimed automatic retry")
	}
}

func TestLocalHostDiagnosticsIncludeServiceFailure(t *testing.T) {
	s := &State{api: newDemoAPI(), clientName: "local", host: Object{
		"deviceId": "local-device", "name": "My Mac", "serviceUrl": "https://example.test/?token=hidden",
		"network": Object{"state": "error", "detail": `WebSocket failed: http2: invalid Upgrade request header ["websocket"]`},
	}}
	report := s.connectionLog("", nil)
	for _, expected := range []string{"Host: local-device", "Name: My Mac", "Service: https://example.test/", "Host transport: error", "http2: invalid Upgrade request header"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("missing %q in host diagnostics: %s", expected, report)
		}
	}
	if strings.Contains(report, "hidden") {
		t.Fatal("host diagnostics leaked credentials")
	}
}

func TestHostConnectionIndicatorReflectsAutomaticServiceRecovery(t *testing.T) {
	s := &State{api: newDemoAPI(), host: Object{}}
	for _, state := range []string{"connecting", "error", "connected", "stopped"} {
		s.host["network"] = Object{"state": state}
		want := state == "connecting" || state == "error"
		if s.connectionRecovering("") != want {
			t.Fatalf("host state %s has wrong recovery indicator", state)
		}
	}
}
