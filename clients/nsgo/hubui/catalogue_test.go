package hubui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type lazyCatalogueAPI struct {
	*demoAPI
	release                    chan struct{}
	syncs, transcripts, models atomic.Int32
}

func (*lazyCatalogueAPI) CanHost() bool { return false }
func (f *lazyCatalogueAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.cached" {
		return nil, nil
	}
	if method == "client.sync" && str(p, "provider") == "codex-app" {
		f.syncs.Add(1)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.release:
		}
		list, err := f.demoAPI.Call(ctx, method, p, target, r)
		return append(objects(list), Object{"id": "older-chat", "providerId": "codex-app", "title": "更早的会话", "cwd": "/workspace/agenthub"}), err
	}
	if method == "client.call" {
		switch str(r, "method") {
		case "transcript":
			f.transcripts.Add(1)
		case "models":
			f.models.Add(1)
		case "watch":
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func TestColdCatalogueIsUsableBeforeFullScanAndMessagesLoadOnOpen(t *testing.T) {
	f := &lazyCatalogueAPI{demoAPI: newDemoAPI(), release: make(chan struct{})}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844))
	app.WaitFor("id:device-demo-device", time.Second)
	app.Press("id:device-demo-device")
	app.WaitFor("id:session-demo-chat", time.Second)
	// A different provider must remain usable while the App directory is blocked.
	app.WaitFor("id:session-demo-cli", time.Second)
	app.WaitUntil(time.Second, func() bool { return f.syncs.Load() > 0 })
	if f.transcripts.Load() != 0 || f.models.Load() != 0 {
		t.Fatal("opening a device eagerly loaded messages/models")
	}
	app.Press("id:session-demo-chat")
	app.WaitUntil(time.Second, func() bool { return f.transcripts.Load() == 1 })
	app.WaitFor("id:message-demo-turn|demo-assistant", time.Second)
	close(f.release)
	app.Press("id:back-sessions")
	app.WaitFor("id:session-older-chat", time.Second)
	if f.transcripts.Load() != 1 {
		t.Fatal("directory completion loaded unrelated transcripts")
	}
}
