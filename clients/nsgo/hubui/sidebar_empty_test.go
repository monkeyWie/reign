package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestSidebarEmptyGuidanceAndRecovery(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, &localDemo{newDemoAPI()})
		s.applyColors()
		s.target = "local"
		s.devices = []Object{{"id": "local", "local": true, "connected": true}}
		s.catalogue("local").loaded = true
		return s.directory(cx)
	}, nsgotest.Size(300, 760), nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	app.Find("id:empty-sessions-action")
	if app.Exists("id:section:all") || app.Exists("id:section:recent") || app.Exists("id:section:pinned") {
		t.Fatal("repeated global empty states")
	}
	app.Screenshot("sidebar-first-use-light")
	app.Press("id:theme-toggle")
	app.Screenshot("sidebar-first-use-dark")
	app.Press("id:empty-sessions-action")
	if s.dialog.GetUntracked() != "local" {
		t.Fatal("onboarding did not lead to local Agents")
	}
	app.Post(func() {
		s.cx.Post(func() {
			s.catalogue("local").polling, s.polling = true, true
			s.changed()
		})
	})
	app.Settle()
	if app.Exists("id:sessions-loading") {
		t.Fatal("periodic refresh flickered the settled empty state")
	}
	app.Post(func() {
		s.cx.Post(func() {
			s.connection("local").issues["catalogue"] = "fixture unavailable"
			s.changed()
		})
	})
	app.WaitFor("text:暂时无法获取会话", time.Second)
	app.Press("id:empty-sessions-action")
	if s.dialog.GetUntracked() != "connection-status" {
		t.Fatal("failure had no recovery path")
	}
	app.Post(func() {
		s.cx.Post(func() {
			delete(s.connection("local").issues, "catalogue")
			s.sessions = []Object{{"id": "first", "providerId": "codex-app", "deviceId": "local", "title": "First session", "cwd": "/fixture"}}
			s.catalogueVersion++
			s.changed()
		})
	})
	app.WaitFor("id:section:all", time.Second)
	app.Find("id:section:recent")
	if app.Exists("id:session-list-placeholder") {
		t.Fatal("empty guidance covered recovered sessions")
	}
	app.Post(func() { s.cx.Post(func() { s.filters.Agents = map[string]bool{"other": true}; s.filtersChanged() }) })
	app.WaitFor("id:session-filter-empty", time.Second)
	if app.Exists("id:session-list-placeholder") || app.Exists("id:section:recent") {
		t.Fatal("filtered result became onboarding or duplicate empty state")
	}
	app.Press("id:clear-session-filters")
	app.WaitFor("id:section:recent", time.Second)
}
