package hubui

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestRunningSessionsShareRotation(t *testing.T) {
	var s *State
	var shown *core.Signal[bool]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		shown = core.NewSignal(cx, true)
		return core.Show{When: shown.Get, Then: func(cx *core.Scope) core.Node { return s.workspaceView(cx) }}
	}, nsgotest.Size(1100, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	sessions := append([]Object(nil), s.sessions...)
	first := "id:running-" + sessionRowID("recent", sessions[0])
	firstID := app.Find(first).ID
	running := []string{first}
	for _, index := range []int{1, 2} {
		app.Advance(310 * time.Millisecond)
		session := copyObject(sessions[index])
		session["status"] = "running"
		app.Post(func() {
			s.cx.Post(func() {
				s.applyEnvironmentPage(str(session, "deviceId"), str(session, "providerId"), []Object{session}, false)
			})
		})
		app.Settle()
		section := "recent"
		if index == 2 {
			section = "pinned"
		}
		running = append(running, "id:running-"+sessionRowID(section, session))
		app.Find(running[len(running)-1])
	}
	if app.Find(first).ID != firstID {
		t.Fatal("joining sessions replaced the existing spinner")
	}
	previous := app.Find(first).Animated.Rotate
	for range 12 {
		app.Advance(150 * time.Millisecond)
		angle := app.Find(first).Animated.Rotate
		if nsgotest.Target() == "" {
			if angle == previous {
				t.Fatal("shared rotation stopped")
			}
			for _, id := range running {
				if math.Abs(float64(app.Find(id).Animated.Rotate-angle)) > .01 {
					t.Fatal("late session started at a different angle")
				}
			}
		}
		previous = angle
	}
	app.Screenshot("sessions-shared-rotation-light")
	sidebarMenuAction(app, "theme-toggle")
	app.Screenshot("sessions-shared-rotation-dark")
	app.Post(func() { s.cx.Post(func() { shown.Set(false) }) })
	app.Settle()
	if s.loadingMotion.users.GetUntracked() != 0 {
		t.Fatal("unmounted rings retained their shared animation")
	}
	app.Advance(200 * time.Millisecond)
	app.Post(func() { s.cx.Post(func() { shown.Set(true) }) })
	app.Settle()
	app.Find(first)
	if nsgotest.Target() == "" {
		env := core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN", ReduceMotion: true}
		app.SetEnvironment(env)
		app.Advance(300 * time.Millisecond)
		for _, id := range running {
			if app.Find(id).Animated.Rotate != 0 {
				t.Fatal("reduced motion still rotates")
			}
		}
		env.ReduceMotion = false
		app.SetEnvironment(env)
		app.Advance(300 * time.Millisecond)
		for _, id := range running {
			if app.Find(id).Animated.Rotate != app.Find(first).Animated.Rotate || app.Find(id).Animated.Rotate == 0 {
				t.Fatal("resuming motion lost synchronization")
			}
		}
	}
}
