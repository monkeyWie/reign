package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSessionRowSurvivesCatalogueRefresh(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	row := sessionRowID("all", s.sessions[0])
	rowID := app.Find("id:" + row).ID
	spinnerID := app.Find("id:running-" + row).ID
	rotation := app.Find("id:running-" + row).Animated.Rotate
	var totalRotation float32
	for i := range 12 {
		app.Post(func() {
			s.cx.Post(func() {
				w := s.workspaces["local"]
				incoming := make([]Object, len(w.sessions))
				for j, entry := range w.sessions {
					incoming[j] = copyObject(entry)
					incoming[j]["updatedAt"] = int64(1794960000000 + i*1000)
				}
				s.applyEnvironmentPage("local", "codex-app", incoming, false)
			})
		})
		app.Advance(150 * time.Millisecond)
		if app.Find("id:"+row).ID != rowID || app.Find("id:running-"+row).ID != spinnerID {
			t.Fatal("directory metadata refresh remounted the row or restarted its spinner")
		}
		if nsgotest.Target() == "" {
			next := app.Find("id:running-" + row).Animated.Rotate
			delta := next - rotation
			if delta < 0 {
				delta += 360
			}
			if delta <= 0 || delta > 90 {
				t.Fatal("spinner stalled or jumped during a metadata refresh", rotation, next)
			}
			totalRotation += delta
			rotation = next
		}
	}
	if nsgotest.Target() == "" && totalRotation < 360 {
		t.Fatal("spinner did not progress through a continuous rotation during refresh")
	}
	indent := globalBounds(app.Find("id:" + row)).Origin.X
	app.Post(func() { s.cx.Post(func() { s.setGrouping(groupAgent) }) })
	app.Settle()
	if globalBounds(app.Find("id:"+row)).Origin.X <= indent {
		t.Fatal("grouping did not update the row indentation")
	}
	app.Post(func() { s.cx.Post(func() { s.setGrouping(groupProject) }) })
	app.Settle()
	if nsgotest.Target() == "macos" {
		return // Native test hover delivery is covered separately from identity.
	}
	app.Hover("id:" + row)
	app.Advance(450 * time.Millisecond)
	app.WaitFor("id:session-hover-card", time.Second)
	cardID := app.Find("id:session-hover-card").ID
	for range 8 {
		app.Post(func() {
			s.cx.Post(func() {
				s.catalogueVersion++
				s.changed()
			})
		})
		app.Advance(150 * time.Millisecond)
		if !app.Exists("id:session-hover-card") || app.Find("id:session-hover-card").ID != cardID {
			t.Fatal("directory refresh dismissed or rebuilt an open tooltip")
		}
	}
	app.Screenshot("session-hover-during-refresh")
}
