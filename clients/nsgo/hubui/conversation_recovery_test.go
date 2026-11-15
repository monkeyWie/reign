package hubui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func conversationFixture(t *testing.T, api API, width core.Points, transcript Object, connected bool) (*nsgotest.App, *State) {
	t.Helper()
	var state *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		state = newState(cx, api)
		state.width.Set(width)
		state.devices = []Object{{"id": "demo-device", "connected": connected}}
		state.target = "demo-device"
		state.selected = Object{"id": "demo-chat", "providerId": "codex-app", "title": "会话"}
		state.model = "test-model"
		state.setTranscript(transcript)
		view := state.conversation(cx, true).(core.View)
		view.StyleFunc = func() core.Style {
			return core.Style{Background: palette(cx).background, Color: palette(cx).foreground}
		}
		return view
	}, nsgotest.Size(width, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	return app, state
}

func TestMobileMultilineMessage320(t *testing.T) { testMobileMultilineMessage(t, 320) }
func TestMobileMultilineMessage390(t *testing.T) { testMobileMultilineMessage(t, 390) }
func TestMobileMultilineMessage720(t *testing.T) { testMobileMultilineMessage(t, 720) }

func testMobileMultilineMessage(t *testing.T, width core.Points) {
	t.Helper()
	text := "发现的问题\n\n1. 网站页脚没有公示任何有效的客服联系方式（如联系邮箱、联系电话或在线客服），用户在其他页面也难以直接找到官方客服渠道。\n\n修改建议\n\n1. 请在网站全站页脚公示至少一种有效的联系方式，例如联系邮箱、联系电话或在线客服入口，方便用户随时联系。\n\n" + "https://example.com/" + strings.Repeat("long-path-", 30) + "\n\n```text\n" + strings.Repeat("long-code-", 30) + "\n```"
	transcript := Object{"turns": []Object{{"id": "layout", "status": "completed", "items": []Object{{"id": "user", "role": "user", "text": text}}}}}
	app, _ := conversationFixture(t, newDemoAPI(), width, transcript, true)
	// Phone simulators use their device's real viewport rather than Size.
	viewport := globalBounds(app.Find("id:conversation"))
	for _, id := range []string{"message-layout|user", "message-body-layout|user", "composer-card"} {
		b := globalBounds(app.Find("id:" + id))
		if b.Origin.X < viewport.Origin.X || b.Origin.X+b.Size.Width > viewport.Origin.X+viewport.Size.Width+.5 {
			t.Fatalf("%s exceeds viewport %v: %v", id, viewport, b)
		}
	}
	app.Screenshot("mobile-multiline-message")
}

type delayedSendAPI struct {
	*demoAPI
	release chan struct{}
	sends   atomic.Int32
	fail    atomic.Bool
}

func (f *delayedSendAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && str(r, "method") == "send" {
		f.sends.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
		if f.fail.Load() {
			return nil, errors.New("发送失败")
		}
		return nil, nil
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestOfflineEditingAndSendLoadingPreserveNewDraft(t *testing.T) {
	f := &delayedSendAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}
	app, s := conversationFixture(t, f, 390, Object{}, false)
	app.Type("id:chat-input", "断线时也能写\n下一行")
	if app.Find("id:chat-input").Props.Disabled || !app.Find("id:send-message").Props.Disabled {
		t.Fatal("offline must disable sending only")
	}
	app.Post(func() { s.cx.Post(func() { s.send("send") }) })
	app.Settle()
	if f.sends.Load() != 0 {
		t.Fatal("offline sent a command")
	}
	app.Post(func() { s.cx.Post(func() { s.devices[0]["connected"] = true; s.changed() }) })
	app.WaitUntil(time.Second, func() bool { return !app.Find("id:send-message").Props.Disabled })
	app.Press("id:send-message")
	app.WaitFor("id:send-loading", time.Second)
	if !app.Find("id:send-message").Props.Disabled || app.Find("id:chat-input").Props.Disabled {
		t.Fatal("pending send must stay visible, block duplicates and allow editing")
	}
	app.Post(func() { s.cx.Post(func() { s.send("send") }) })
	app.Screenshot("send-loading")
	app.Clear("id:chat-input")
	app.Type("id:chat-input", "下一条草稿")
	close(f.release)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:send-loading") })
	if f.sends.Load() != 1 || app.Find("id:chat-input").Text != "下一条草稿" {
		t.Fatal("duplicate command or new draft was lost")
	}
	app.Screenshot("send-loading-draft-preserved")
}

func TestSendFailureStopsLoadingAndKeepsDraft(t *testing.T) {
	f := &delayedSendAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}
	f.fail.Store(true)
	app, _ := conversationFixture(t, f, 390, Object{}, true)
	app.Type("id:chat-input", "发送失败也不能丢")
	app.Press("id:send-message")
	app.WaitFor("id:send-loading", time.Second)
	close(f.release)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:send-loading") })
	if app.Find("id:chat-input").Text != "发送失败也不能丢" || app.Find("id:send-message").Props.Disabled {
		t.Fatal("failed send lost draft or stayed disabled")
	}
}

type replayAPI struct {
	*demoAPI
	offline, recovered, delivered, wrongCursor atomic.Bool
}

const initialLiveText = "已收到的回复🙂"

func (f *replayAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.reconnect" {
		f.offline.Store(false)
		f.recovered.Store(true)
	}
	if method == "client.call" && str(r, "method") == "watch" {
		if f.offline.Load() {
			return nil, errors.New("设备离线")
		}
		if f.recovered.Load() && !f.delivered.Load() {
			if str(r, "epoch") != "replay" || number(r, "after") != 11 {
				f.wrongCursor.Store(true)
			}
			f.delivered.Store(true)
			return Object{"epoch": "replay", "cursor": 12, "events": []Object{{"kind": "text", "turnId": "live", "itemId": "answer", "offset": len(utf16.Encode([]rune(initialLiveText))), "delta": "，重连后继续输出"}}}, nil
		}
		if str(r, "epoch") == "" {
			return Object{"epoch": "replay", "cursor": 10, "reset": true}, nil
		}
		if number(r, "after") == 10 {
			return Object{"epoch": "replay", "cursor": 11, "events": []Object{{"kind": "text", "turnId": "live", "itemId": "answer", "offset": 0, "delta": initialLiveText}}}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return Object{"epoch": "replay", "cursor": number(r, "after")}, nil
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestReconnectPreservesLiveReplyAndReplaysFromCursor(t *testing.T) {
	f := &replayAPI{demoAPI: newDemoAPI()}
	app, s := conversationFixture(t, f, 390, Object{}, true)
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitFor("text:"+initialLiveText, time.Second)
	app.WaitUntil(time.Second, func() bool { app.Settle(); return s.streamCursor == 11 })
	f.offline.Store(true)
	app.Post(func() { s.cx.Post(func() { s.devices[0]["connected"] = false; s.changed() }) })
	app.WaitFor("role:button 尚未连接，点击重试", time.Second)
	// Return to the foreground while offline: transcript reads succeed from an
	// older cache, but must not erase the visible streamed reply.
	app.Post(func() { s.cx.Post(s.startSessionWatch) })
	app.WaitFor("text:"+initialLiveText, time.Second)
	app.Post(func() { s.cx.Post(func() { s.reconnectDevice("demo-device") }) })
	app.WaitFor("text:"+initialLiveText+"，重连后继续输出", 3*time.Second)
	if f.wrongCursor.Load() {
		t.Fatal("reconnect discarded the replay cursor")
	}
	app.Screenshot("reconnected-live-reply")
}
