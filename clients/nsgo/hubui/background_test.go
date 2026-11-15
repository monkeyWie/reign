package hubui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type backgroundTaskFixture struct {
	*demoAPI
	finished     chan struct{}
	monitorReads atomic.Int32
}

func (f *backgroundTaskFixture) Call(ctx context.Context, method string, p Object, target string, request Object) (any, error) {
	if method == "client.call" && str(request, "session") == "demo-chat" {
		switch str(request, "method") {
		case "transcript":
			f.monitorReads.Add(1)
			status := "inProgress"
			select {
			case <-f.finished:
				status = "completed"
			default:
			}
			return Object{"session": f.sessions[0], "turns": []Object{{"id": "live", "status": status, "items": []Object{{"id": "answer", "role": "assistant", "text": "任务输出"}}}}}, nil
		case "watch":
			if flag(request, "probe") {
				return Object{"epoch": "tasks", "cursor": number(request, "after"), "reset": str(request, "epoch") != "tasks"}, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.finished:
				if number(request, "after") > 0 {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return Object{"epoch": "tasks", "cursor": 1, "events": []Object{{"kind": "turn", "turnId": "live", "status": "completed"}}}, nil
			}
		}
	}
	return f.demoAPI.Call(ctx, method, p, target, request)
}

func TestAndroidTaskCompletesAfterSwitchingChats(t *testing.T) {
	if isWeb() || nsgotest.Target() != "" {
		t.Skip("native host injection")
	}
	f := &backgroundTaskFixture{demoAPI: newDemoAPI(), finished: make(chan struct{})}
	host := core.NewHost()
	var latest atomic.Value
	host.Handle("ras.background", func(call *core.HostCall) {
		latest.Store(object(call.Data))
		call.Resolve(Object{"running": flag(object(call.Data), "enabled")})
	})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid}), nsgotest.Host(host))
	openDemoChatTask := func() {
		app.WaitFor("id:device-demo-device", time.Second)
		app.Press("id:device-demo-device")
		app.WaitFor("id:session-demo-chat", time.Second)
		app.Press("id:session-demo-chat")
		app.WaitFor("text:任务输出", time.Second)
	}
	openDemoChatTask()
	status := func() string {
		for _, task := range objects(object(latest.Load())["tasks"]) {
			if str(task, "id") == "demo-chat" {
				return str(task, "status")
			}
		}
		return ""
	}
	app.WaitUntil(time.Second, func() bool { return status() == "inProgress" })
	before := f.monitorReads.Load()
	app.Press("id:back-sessions")
	app.Press("id:session-demo-cli")
	app.WaitUntil(time.Second, func() bool { return f.monitorReads.Load() > before })
	close(f.finished)
	app.WaitUntil(2*time.Second, func() bool { return status() == "completed" })
}

func TestAndroidForegroundServiceRetainsStreamsOnBackground(t *testing.T) {
	if isWeb() || nsgotest.Target() != "" {
		t.Skip("requires native host lifecycle injection")
	}
	f := &connectionFixture{demoAPI: newDemoAPI()}
	f.blockWatch.Store(true)
	f.autoReconnect.Store(true)
	host := core.NewHost()
	var started atomic.Bool
	host.Handle("ras.background", func(call *core.HostCall) {
		enabled := flag(object(call.Data), "enabled")
		started.Store(enabled)
		call.Resolve(Object{"running": enabled})
	})
	env := core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(env), nsgotest.Host(host))
	openDemoChat(t, app)
	app.WaitUntil(time.Second, func() bool {
		return started.Load() && app.NotificationPermission == core.NotificationsGranted && f.watchStarts.Load() > 0
	})
	app.Type("id:chat-input", "保留后台草稿")
	before := f.watchStarts.Load()
	env.Lifecycle = core.LifecycleBackground
	app.SetEnvironment(env)
	time.Sleep(100 * time.Millisecond)
	app.Settle()
	if f.watchCancels.Load() != 0 {
		t.Fatal("Android suspended a stream protected by its foreground service")
	}
	env.Lifecycle = core.LifecycleActive
	app.SetEnvironment(env)
	app.Settle()
	if f.watchStarts.Load() != before || f.reconnects.Load() != 0 || app.Find("id:chat-input").Text != "保留后台草稿" {
		t.Fatal("resume replaced a healthy connection or draft")
	}
	// Losing the native service must fall back to the normal safe suspend /
	// resync behavior, rather than claiming background protection still exists.
	env.Lifecycle = core.LifecycleBackground
	app.SetEnvironment(env)
	if err := host.Send("ras.background-state", Object{"running": false}); err != nil {
		t.Fatal(err)
	}
	app.WaitUntil(time.Second, func() bool { return f.watchCancels.Load() > 0 })
}

func TestAndroidTaskStatusIgnoresHistoricalCompletions(t *testing.T) {
	if isWeb() {
		t.Skip("native notifications")
	}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		return core.View{}
	}, nsgotest.Environment(core.Environment{OS: core.OSAndroid}))
	_ = app
	transcript := func(id, status string) Object {
		return Object{"session": Object{"title": "测试会话"}, "turns": []Object{{"id": id, "status": status}}}
	}
	s.trackNativeTask("host", "codex-app", "session", transcript("old", "completed"))
	if s.lastTask != nil {
		t.Fatal("historical completion generated a notification")
	}
	s.trackNativeTask("host", "codex-app", "session", transcript("live", "inProgress"))
	s.trackNativeTask("host", "codex-app", "session", transcript("live", "completed"))
	if str(s.lastTask, "label") != "已完成" {
		t.Fatal("live completion did not update its running notification")
	}
	s.trackNativeTask("host", "codex-app", "session", transcript("live", "inProgress"))
	if str(s.lastTask, "status") != "completed" {
		t.Fatal("stale running state reversed a completed notification")
	}
	s.trackNativeTask("other-device", "codex-app", "session", transcript("live", "inProgress"))
	if len(s.nativeTasks) != 2 {
		t.Fatal("notifications for different devices collided")
	}
}
