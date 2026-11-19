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

type backgroundTaskFixture struct {
	*demoAPI
	finished     chan struct{}
	monitorReads atomic.Int32
	stateReads   atomic.Int32
	watchReads   atomic.Int32
	heartbeats   chan struct{}
}

func (f *backgroundTaskFixture) Call(ctx context.Context, method string, p Object, target string, request Object) (any, error) {
	if method == "client.call" && str(request, "session") == "demo-chat" {
		switch str(request, "method") {
		case "turn.status":
			f.stateReads.Add(1)
			status := "inProgress"
			select {
			case <-f.finished:
				status = "completed"
			default:
			}
			return Object{"id": "live", "status": status}, nil
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
			f.watchReads.Add(1)
			if flag(request, "probe") {
				return Object{"epoch": "tasks", "cursor": number(request, "after"), "reset": str(request, "epoch") != "tasks"}, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.heartbeats:
				return Object{"epoch": "tasks", "cursor": 0, "heartbeat": true}, nil
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
	f := &backgroundTaskFixture{demoAPI: newDemoAPI(), finished: make(chan struct{}), heartbeats: make(chan struct{}, 1)}
	host := core.NewHost()
	var latest atomic.Value
	host.Handle("ras.background", func(call *core.HostCall) {
		latest.Store(object(call.Data))
		call.Resolve(Object{"running": flag(object(call.Data), "enabled")})
	})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{OS: core.OSAndroid}), nsgotest.Host(host))
	openDemoChatTask := func() {
		app.WaitFor("id:device-demo-device", time.Second)
		app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), time.Second)
		app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
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
	before := f.stateReads.Load()
	app.Press("id:back-sessions")
	app.Press(sessionSelector("demo-device", "pi", "demo-pi"))
	app.WaitUntil(time.Second, func() bool { return f.stateReads.Load() > before })
	historyReads := f.monitorReads.Load()
	for range 3 {
		before := f.stateReads.Load()
		watches := f.watchReads.Load()
		f.heartbeats <- struct{}{}
		app.WaitUntil(time.Second, func() bool { return f.watchReads.Load() > watches })
		app.WaitUntil(time.Second, func() bool { return f.stateReads.Load() > before })
	}
	if f.monitorReads.Load() != historyReads {
		t.Fatal("background heartbeat downloaded conversation history")
	}
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
	// Losing foreground-service protection does not stop synchronization while
	// the OS still lets this process run. Recovery belongs to actual failures.
	env.Lifecycle = core.LifecycleBackground
	app.SetEnvironment(env)
	if err := host.Send("ras.background-state", Object{"running": false}); err != nil {
		t.Fatal(err)
	}
	app.Settle()
	if f.watchCancels.Load() != 0 || f.watchStarts.Load() != before {
		t.Fatal("foreground-service loss interrupted a healthy watch")
	}
}

func TestAndroidForegroundServiceDenialKeepsSynchronization(t *testing.T) {
	if isWeb() || nsgotest.Target() != "" {
		t.Skip("requires native host lifecycle injection")
	}
	f := &connectionFixture{demoAPI: newDemoAPI()}
	f.blockWatch.Store(true)
	host := core.NewHost()
	var attempts atomic.Int32
	host.Handle("ras.background", func(call *core.HostCall) {
		attempts.Add(1)
		call.Reject(errors.New("foreground service unavailable"))
	})
	env := core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return RootWithAPI(cx, f, true) }, nsgotest.Size(390, 844), nsgotest.Environment(env), nsgotest.Host(host))
	openDemoChat(t, app)
	app.WaitUntil(time.Second, func() bool { return attempts.Load() > 0 && f.watchStarts.Load() > 0 })
	env.Lifecycle = core.LifecycleBackground
	app.SetEnvironment(env)
	f.version.Store(1)
	env.Lifecycle = core.LifecycleActive
	app.SetEnvironment(env)
	app.WaitFor("id:message-resumed|latest", 2*time.Second)
	if f.watchCancels.Load() != 0 || f.reconnects.Load() != 0 || f.sends.Load() != 0 {
		t.Fatal("denied foreground service cancelled synchronization or replayed a command")
	}
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

func TestBackgroundServiceSurvivesRecoveryPauseWithoutAnActiveReply(t *testing.T) {
	f := &connectionFixture{demoAPI: newDemoAPI()}
	app, s := conversationFixture(t, f, 390, Object{}, false)
	checked := make(chan bool, 1)
	app.Post(func() {
		s.cx.Post(func() {
			s.devices[0]["autoReconnect"] = true
			s.connection("demo-device").established = true
			// A failed attempt is waiting for the short retry pause. No active reply.
			s.connection("demo-device").reconnecting = false
			status := s.nativeBackgroundStatus()
			kept := flag(status, "enabled")
			// Explicitly disconnecting ends the service and its protection.
			s.devices[0]["autoReconnect"] = false
			stopped := !flag(s.nativeBackgroundStatus(), "enabled")
			checked <- kept && stopped
		})
	})
	app.WaitUntil(time.Second, func() bool {
		select {
		case ok := <-checked:
			if !ok {
				t.Fatal("foreground service stopped during recovery or survived explicit disconnect")
			}
			return true
		default:
			return false
		}
	})
}
