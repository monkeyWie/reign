package hubui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type cachedWorkspaceAPI struct {
	*demoAPI
	online, noCache     atomic.Bool
	connects, liveReads atomic.Int32
	connectResult       chan error
	liveRelease         chan struct{}
}

func (*cachedWorkspaceAPI) CanHost() bool { return false }
func (f *cachedWorkspaceAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	switch method {
	case "client.list":
		return []Object{{"id": "demo-device", "name": "工作电脑", "connectionId": "123456", "service": DefaultService, "connected": f.online.Load(), "autoReconnect": true}}, nil
	case "client.cached":
		if f.noCache.Load() {
			return nil, nil
		}
		if str(r, "method") == "models" {
			return []Object{{"id": "gpt-5.4", "name": "GPT-5.4"}, {"id": "gpt-5.3-codex", "name": "GPT-5.3 Codex"}}, nil
		}
		if str(r, "method") == "transcript" {
			return f.cachedTranscript("本地保存的完整消息"), nil
		}
		return Object{"providers": demoProviders(), "sessions": f.sessions}, nil
	case "client.reconnect":
		f.connects.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-f.connectResult:
			if err != nil {
				return nil, err
			}
			f.online.Store(true)
			return f.Call(ctx, "client.list", nil, "", nil)
		}
	case "client.snapshot":
		return nil, nil
	case "client.call":
		if str(r, "method") == "watch" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if str(r, "method") == "providers" || str(r, "method") == "transcript" {
			f.liveReads.Add(1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.liveRelease:
			}
			if str(r, "method") == "transcript" {
				return f.cachedTranscript("后台同步到的最新消息"), nil
			}
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func (f *cachedWorkspaceAPI) cachedTranscript(text string) Object {
	return Object{"session": f.sessions[0], "turns": []Object{{"id": "cached", "status": "completed", "items": []Object{{"id": "reply", "role": "assistant", "text": text}}}}}
}
func startCachedWorkspace(t *testing.T, connected bool) (*nsgotest.App, *cachedWorkspaceAPI) {
	t.Helper()
	f := &cachedWorkspaceAPI{demoAPI: newDemoAPI(), connectResult: make(chan error, 1), liveRelease: make(chan struct{})}
	f.online.Store(connected)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844))
	app.WaitFor("id:device-demo-device", time.Second)
	return app, f
}

func TestSavedDeviceOpensCachedMessagesWhileConnectingAndAfterFailure(t *testing.T) {
	app, f := startCachedWorkspace(t, false)
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-demo-chat", time.Second)
	if app.Exists("id:dialog-connecting") || app.Exists("id:dialog-connection-status") {
		t.Fatal("navigation opened a blocking connection panel")
	}
	app.Press("id:session-demo-chat")
	app.WaitFor("text:本地保存的完整消息", time.Second)
	app.Find("id:connection-retrying")
	app.Type("id:chat-input", "离线也可以编辑草稿")
	app.Press("id:model-selector")
	app.Press("role:button GPT-5.3 Codex")
	if app.Find("id:chat-input").Props.Disabled || !app.Find("id:send-message").Props.Disabled || f.liveReads.Load() != 0 {
		t.Fatal("connection gated editing or initiated reads before connecting")
	}
	f.connectResult <- errors.New("设备离线")
	app.WaitFor("role:button 尚未连接，点击重试", time.Second)
	app.Find("text:本地保存的完整消息")
	if app.Exists("text:设备离线，显示本地缓存；连接恢复后同步最新消息。") {
		t.Fatal("connection status must stay in the header, not below messages")
	}
	if app.Exists("id:error-banner") || app.Find("id:chat-input").Text != "离线也可以编辑草稿" {
		t.Fatal("connection failure replaced cached content or draft")
	}
	app.Screenshot("cached-chat-while-disconnected")
	// A failed initial attempt must not become an infinite retry loop.
	time.Sleep(2100 * time.Millisecond)
	app.Settle()
	if f.connects.Load() != 1 {
		t.Fatal("failed initial connection retried automatically")
	}
}

func TestConnectedSlowPeerShowsCacheBeforeLiveReadsComplete(t *testing.T) {
	app, f := startCachedWorkspace(t, true)
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-demo-chat", time.Second)
	app.Press("id:session-demo-chat")
	app.WaitFor("text:本地保存的完整消息", time.Second)
	app.WaitUntil(time.Second, func() bool { return f.liveReads.Load() >= 2 })
	if app.Exists("id:transcript-loading") {
		t.Fatal("cached messages hidden behind network loading")
	}
	close(f.liveRelease)
	app.WaitFor("text:后台同步到的最新消息", time.Second)
	if app.Exists("text:本地保存的完整消息") {
		t.Fatal("live state failed to replace completed cached turn")
	}
}

func TestNoCachedSessionsShowsLoadingAndStopsAfterConnectionFailure(t *testing.T) {
	app, f := startCachedWorkspace(t, false)
	f.noCache.Store(true)
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-sidebar", time.Second)
	app.WaitFor("id:sessions-loading", time.Second)
	f.connectResult <- errors.New("设备离线")
	app.WaitFor("text:暂无本地会话，连接后会自动同步。", time.Second)
	if app.Exists("id:sessions-loading") || app.Exists("id:dialog-connecting") {
		t.Fatal("failure left an endless loading or modal")
	}
	app.Screenshot("empty-cache-offline")
}

type lateSnapshotAPI struct {
	*cachedWorkspaceAPI
	started, release chan struct{}
}

func (f *lateSnapshotAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.cached" && str(r, "method") == "transcript" {
		close(f.started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
	}
	return f.cachedWorkspaceAPI.Call(ctx, method, p, target, r)
}
func TestLateDiskSnapshotCannotOverwriteNewerLiveMessages(t *testing.T) {
	base := &cachedWorkspaceAPI{demoAPI: newDemoAPI(), connectResult: make(chan error, 1), liveRelease: make(chan struct{})}
	base.online.Store(true)
	close(base.liveRelease)
	f := &lateSnapshotAPI{cachedWorkspaceAPI: base, started: make(chan struct{}), release: make(chan struct{})}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844))
	app.WaitFor("id:device-demo-device", time.Second)
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-demo-chat", time.Second)
	app.Press("id:session-demo-chat")
	app.WaitFor("text:后台同步到的最新消息", time.Second)
	close(f.release)
	app.WaitUntil(time.Second, func() bool {
		select {
		case <-f.started:
			return true
		default:
			return false
		}
	})
	app.Settle()
	if app.Exists("text:本地保存的完整消息") {
		t.Fatal("late cache replaced live messages")
	}
}
