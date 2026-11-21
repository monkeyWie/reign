package hubui

import (
	"context"
	_ "github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"testing"
	"time"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }
func startDemo(t *testing.T, w, h core.Points) *nsgotest.App {
	t.Helper()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, &localDemo{newDemoAPI()}, true) }, nsgotest.Size(w, h), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	showSessionDirectory(t, app)
	return app
}
func showSessionDirectory(t *testing.T, app *nsgotest.App) {
	t.Helper()
	if app.Exists("id:mobile-menu") && !app.Find("id:mobile-menu").Props.Expanded {
		app.WaitUntil(time.Second, func() bool { return !app.Exists("id:mobile-sidebar") })
		app.Press("id:mobile-menu")
	}
	app.WaitFor("id:session-sidebar", 3*time.Second)
}

func openDemoChat(t *testing.T, app *nsgotest.App) {
	t.Helper()
	showSessionDirectory(t, app)
	app.WaitFor("id:device-demo-device", 3*time.Second)
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), 5*time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitFor("id:chat-input", 3*time.Second)
	app.WaitFor("id:message-demo-turn|demo-assistant", 5*time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:mobile-sidebar") })
}
func TestDesktopNavigationAndDrafts(t *testing.T) {
	app := startDemo(t, 1320, 880)
	app.WaitFor("id:session-sidebar", 3*time.Second)
	app.Screenshot("desktop-dashboard")
	openDemoChat(t, app)
	if width := app.Find("id:session-sidebar").Frame.Size.Width; width != 300 {
		t.Fatalf("session sidebar width = %v", width)
	}
	app.Type("id:chat-input", "未发送的草稿")
	inputID := app.Find("id:chat-input").ID
	// The two-second catalogue poll must preserve the mounted input and draft.
	time.Sleep(2100 * time.Millisecond)
	app.Settle()
	if app.Find("id:chat-input").ID != inputID {
		t.Fatal("poll rebuilt editor")
	}
	app.Press(sessionSelector("demo-device", "pi", "demo-pi"))
	app.WaitFor("id:chat-input", 3*time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(3*time.Second, func() bool { return app.Find("id:chat-input").Text == "未发送的草稿" })
	app.Screenshot("desktop-conversation")
	sidebarMenuAction(app, "theme-toggle")
	app.Screenshot("desktop-dark")
	app.Press("id:model-selector")
	app.Find("role:button GPT-5.3 Codex")
	app.Press("role:button GPT-5.3 Codex")
	app.Find("text:GPT-5.3 Codex")
	app.Press("id:connect-device")
	app.Find("id:dialog-connect")
	app.Press("id:dialog-cancel")
}
func TestPhoneNavigation(t *testing.T) {
	app := startDemo(t, 390, 844)
	openDemoChat(t, app)
	if app.Exists("id:session-sidebar") {
		t.Fatal("phone shows conversation and sessions at once")
	}
	if width := app.Find("id:composer-card").Frame.Size.Width; width > 390 || width < 350 {
		t.Fatal("composer overflows phone")
	}
	if app.Exists("id:device-sidebar") {
		t.Fatal("phone chat retains device sidebar")
	}
	app.Type("id:chat-input", "手机草稿")
	app.Screenshot("phone-conversation")
	app.Press("id:mobile-menu")
	app.Find("id:session-sidebar")
	app.Screenshot("phone-sessions")
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return app.Find("id:chat-input").Text == "手机草稿" })
	pressWorkspaceBack(t, app)
	app.Find("id:chat-input")
	showSessionDirectory(t, app)
	pressWorkspaceBack(t, app)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:mobile-sidebar") })
	showSessionDirectory(t, app)
	app.Press("id:device-demo-device")
	app.Find("id:dialog-device")
	app.Press("id:dialog-device-close")
	app.Find("id:session-sidebar")
}
func TestComposerSubmit(t *testing.T) {
	app := startDemo(t, 1320, 880)
	openDemoChat(t, app)
	app.Type("id:chat-input", "这是演示测试")
	app.Submit("id:chat-input")
	app.WaitUntil(3*time.Second, func() bool { return app.Find("id:chat-input").Text == "" })
}

func TestDeviceCopyFeedback(t *testing.T) {
	app := startDemo(t, 1320, 880)
	app.WaitFor("id:session-sidebar", 3*time.Second)
	app.Press("id:device-local")
	app.WaitFor("id:device-code", 3*time.Second)
	app.WaitUntil(3*time.Second, func() bool { return app.Find("id:device-code").Text == "266 556" })
	app.Press("id:copy-device-id")
	if app.Clipboard != "266556" {
		t.Fatalf("copied %q, want unformatted device ID", app.Clipboard)
	}
	if app.Exists("id:notice") {
		t.Fatal("copy must not show a corner notification")
	}
	if app.Find("id:copy-device-id").Props.Label != "已复制" {
		t.Fatal("copy button did not show confirmation")
	}
	app.Screenshot("device-copy-feedback")
	app.FastForward(2 * time.Second)
	if app.Find("id:copy-device-id").Props.Label != "复制 ID" || app.Exists("id:notice") {
		t.Fatal("copy confirmation did not expire")
	}
	app.Press("id:copy-device-id")
	if app.Find("id:copy-device-id").Props.Label != "已复制" {
		t.Fatal("copy button did not show confirmation")
	}
	app.FastForward(time.Second)
	app.Press("id:copy-device-id")
	app.FastForward(time.Second)
	if app.Find("id:copy-device-id").Props.Label != "已复制" {
		t.Fatal("previous copy timer cleared the replacement confirmation")
	}
	app.FastForward(time.Second)
	if app.Find("id:copy-device-id").Props.Label != "复制 ID" || app.Exists("id:notice") {
		t.Fatal("replacement copy confirmation did not expire")
	}
}

func TestLiveStatusAndEditorStability(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s := newState(cx, newDemoAPI())
		s.devices = []Object{{"id": "fixture", "connected": true}}
		s.target = "fixture"
		s.selected = Object{"id": "fixture-session", "providerId": "codex-app"}
		s.models = []Object{{"id": "test", "name": "Test model"}}
		s.model = "test"
		turn := Object{"id": "t", "status": "inProgress", "items": []Object{{"id": "u", "role": "user", "text": "测试输入"}}}
		s.setTranscript(Object{"turns": []Object{turn}})
		return core.View{Style: fill, Children: []core.Node{button(cx, "fixture-output", "输出正文", nil, "outline", func() {
			turn["items"] = []Object{{"id": "u", "role": "user", "text": "测试输入"}, {"id": "a", "role": "assistant", "text": "正在检查文件。"}, {"id": "tool", "role": "tool", "status": "inProgress", "activities": []Object{{"kind": "read", "title": "读取文件", "detail": "fixture.go"}}}}
			s.setTranscript(Object{"turns": []Object{turn}})
		}), button(cx, "fixture-complete", "完成", nil, "outline", func() { turn["status"] = "completed"; s.setTranscript(Object{"turns": []Object{turn}}) }), s.conversation(cx, false)}}
	}, nsgotest.Size(900, 800))
	if !app.Exists("text:Thinking…") {
		t.Fatal("initial thinking missing")
	}
	app.Type("id:chat-input", "保留输入")
	before := app.Find("id:chat-input").ID
	app.Press("id:fixture-output")
	app.Find("id:tool-group")
	if app.Exists("text:Thinking…") {
		t.Fatal("thinking persisted after public output")
	}
	if app.Exists("id:message-actions-t|a") {
		t.Fatal("live output has copy footer")
	}
	if app.Exists("text:fixture.go") {
		t.Fatal("tool details are expanded by default")
	}
	if app.Find("id:chat-input").ID != before || app.Find("id:chat-input").Text != "保留输入" {
		t.Fatal("stream rebuilt input")
	}
	app.Press("role:button 正在读取文件")
	app.Find("text:fixture.go")
	app.Screenshot("live-activity")
	app.Press("id:fixture-complete")
	if app.Exists("id:tool-group") {
		t.Fatal("completed turn retained tool details")
	}
	app.Hover("id:message-t|a")
	app.Find("id:message-actions-t|a")
}

func sessionSelector(target, agent, id string) string {
	return "id:" + sessionRowID("all", Object{"deviceId": target, "providerId": agent, "id": id})
}

type localDemo struct{ *demoAPI }

func (d *localDemo) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	v, err := d.demoAPI.Call(ctx, method, p, target, r)
	if method == "client.list" {
		return append([]Object{{"id": "local", "name": "本机", "local": true, "connected": true}}, objects(v)...), err
	}
	return v, err
}
