package hubui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type conversationEntryAPI struct{ *independentStreamAPI }

func (f *conversationEntryAPI) CanHost() bool { return false }
func (f *conversationEntryAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.cached" && str(r, "method") == "transcript" {
		return nil, nil
	}
	return f.independentStreamAPI.Call(ctx, method, p, target, r)
}
func TestMobileSessionEntryStartsAtLatestAfterDelayedHistory(t *testing.T) {
	f := &conversationEntryAPI{&independentStreamAPI{demoAPI: newDemoAPI(), snapshots: make(chan snapshotRequest, 4), events: make(chan Object, 8), approvalsStarted: make(chan struct{})}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	app.WaitFor("id:device-demo-device", time.Second)
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return len(f.snapshots) > 0 })
	app.WaitFor("id:transcript-loading", time.Second)
	request := <-f.snapshots
	request.reply <- outgoingTranscript(Object{"id": "old", "role": "user", "text": "older instruction"}, Object{"id": "latest", "role": "assistant", "text": strings.Repeat("a long historical reply\n\n", 120) + "end of latest reply"})
	assertBottom := func() {
		t.Helper()
		app.WaitUntil(2*time.Second, func() bool {
			list := app.Find("id:chat-message-list")
			return list.Content.Height > list.Frame.Size.Height && list.Content.Height-list.Frame.Size.Height-list.Offset.Y <= 1 && !app.Exists("id:jump-to-latest")
		})
	}
	assertBottom()
	app.Scroll("id:chat-message-list", -250)
	app.WaitFor("id:jump-to-latest", time.Second)
	app.Press("id:back-sessions")
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	assertBottom()
	app.Screenshot("mobile-session-entry-at-latest")
}
