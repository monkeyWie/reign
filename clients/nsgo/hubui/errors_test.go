package hubui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

const writerConflict = "无法接续原会话（不会另建会话）: thread fixture already has an active writer (RPC -32600)"

func TestSessionErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name, raw, provider, source, method string
		guided, app                         bool
	}{
		{"app", writerConflict, "codex-app", "app", "send", true, true},
		{"app over CLI route", writerConflict, "codex-cli", "app", "send", true, true},
		{"CLI", writerConflict, "codex-cli", "cli", "send", true, false},
		{"other provider", writerConflict, "claude-code", "custom", "send", false, false},
		{"generic invalid request", "invalid request (RPC -32600)", "codex-app", "app", "send", false, false},
		{"network error", "device offline", "codex-app", "app", "send", false, false},
		{"interrupt", writerConflict, "codex-app", "app", "interrupt", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &sessionOperationError{cause: errors.New(tc.raw), target: "remote", device: "工作电脑", provider: tc.provider, source: tc.source, method: tc.method}
			h := sessionAccessHelp(tc.raw, f)
			if (h.Title != "") != tc.guided {
				t.Fatalf("unexpected guidance: %#v", h)
			}
			if tc.guided && (!strings.Contains(h.Steps[0], "工作电脑") || (len(h.Steps) == 3) != tc.app) {
				t.Fatalf("wrong device or source guidance: %#v", h)
			}
		})
	}
}

type writerConflictAPI struct {
	*demoAPI
	sends, activations atomic.Int32
}

func (*writerConflictAPI) CanHost() bool { return false }
func (f *writerConflictAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "host.activate" || method == "host.enable" {
		f.activations.Add(1)
	}
	if method == "client.call" && str(r, "method") == "send" {
		f.sends.Add(1)
		return nil, errors.New(writerConflict)
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestSessionWriterConflictGuidance(t *testing.T) {
	testSessionWriterConflictGuidance(t, 1320, 880)
}

func TestSessionWriterConflictOnPhone(t *testing.T) {
	testSessionWriterConflictGuidance(t, 390, 844)
}

func globalBounds(v *nsgotest.View) core.Bounds {
	b := v.Frame
	for p := v.Parent; p != nil; p = p.Parent {
		b.Origin.X += p.Frame.Origin.X - p.Offset.X
		b.Origin.Y += p.Frame.Origin.Y - p.Offset.Y
	}
	return b
}

func testSessionWriterConflictGuidance(t *testing.T, width, height core.Points) {
	t.Helper()
	f := &writerConflictAPI{demoAPI: newDemoAPI()}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(width, height), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	openDemoChat(t, app)
	app.Type("id:chat-input", "保留这段未发送的内容")
	conversation := app.Find("id:conversation").Frame
	list := app.Find("id:chat-message-list").Frame
	composer := globalBounds(app.Find("id:composer-card"))
	app.Submit("id:chat-input")
	app.WaitFor("text:此会话暂时无法发送消息", 3*time.Second)
	if app.Find("id:conversation").Frame != conversation || app.Find("id:chat-message-list").Frame != list || globalBounds(app.Find("id:composer-card")) != composer {
		t.Fatal("error moved the conversation, message list or composer")
	}
	card := globalBounds(app.Find("id:error-banner"))
	if card.Origin.Y+card.Size.Height > composer.Origin.Y || card.Size.Width > 520 || card.Origin.Y < 0 || card.Origin.X < 0 || card.Origin.X+card.Size.Width > width {
		t.Fatalf("error must float above composer and stay inside window: error=%v composer=%v", card, composer)
	}
	app.Screenshot(choose(width < 650, "phone-session-error-overlay", "session-error-overlay"))
	if app.Exists("id:session-error-raw") {
		t.Fatal("raw error is expanded by default")
	}
	if app.Find("id:chat-input").Text != "" {
		t.Fatal("failed send lost the draft")
	}
	app.Press("id:session-error-details")
	app.Find("id:session-error-raw")
	if app.Find("id:chat-message-list").Frame != list || globalBounds(app.Find("id:composer-card")) != composer {
		t.Fatal("expanded error details moved the conversation")
	}
	app.Press("id:session-error-details")
	app.Press("id:session-access-help")
	app.Find("id:dialog-session-access-help")
	app.Find("text:2. 先等待 Codex 当前任务结束，再在「Codex App」卡片点击「启用」，按提示正常退出并重新打开 Codex App。")
	app.Screenshot(choose(width < 650, "phone-session-access-guidance", "session-access-guidance"))
	app.Press("id:session-help-done")
	if f.sends.Load() != 1 || f.activations.Load() != 0 {
		t.Fatal("guidance retried the message or activated an Agent")
	}
	app.Press("id:dismiss-error")
	if app.Exists("id:error-banner") || app.Find("id:chat-input").Text != "" {
		t.Fatal("dismiss lost the draft or left the error visible")
	}
}
