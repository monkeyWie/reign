package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestCopyFeedbackTimerStopsWithWorkspace(t *testing.T) {
	var s *State
	var mounted *core.Signal[bool]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		mounted = core.NewSignal(cx, true)
		return core.Show{When: mounted.Get, Then: func(cx *core.Scope) core.Node {
			s = newState(cx, newDemoAPI())
			return s.copyButton(cx, "copy-fixture", "Copy", "ghost", func() string { return "fixture" })
		}}
	})
	app.Press("id:copy-fixture")
	app.Find("role:button 已复制")
	app.Post(func() { s.cx.Post(func() { mounted.Set(false) }) })
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:copy-fixture") })
	app.FastForward(3 * time.Second)
	if s.copiedValue != "fixture" {
		t.Fatal("copy timer mutated the disposed workspace")
	}
}
