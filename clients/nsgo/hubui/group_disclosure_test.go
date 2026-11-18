package hubui

import (
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestGroupInformationAndFolderDisclosure(t *testing.T) {
	var s *State
	var group directoryRow
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		for _, r := range s.directoryRows() {
			if r.Kind == "project" {
				group = r
				break
			}
		}
		return core.View{Style: fill, Children: []core.Node{core.View{Style: core.Style{Width: core.Pt(300), Height: core.Pct(100)}, Children: []core.Node{s.directory(cx)}}}}
	}, nsgotest.Size(800, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Locale: "zh-CN"}))
	selector := "id:" + group.ID
	child := "id:" + sessionRowID("all", s.sessions[0])
	if app.Exists("text:2") || app.Exists("id:group-info-card") {
		t.Fatal("group count is visible at rest")
	}
	openIcon := app.Find(selector + "-icon").Drawing
	app.Press(selector)
	if app.Exists(child) || app.Find(selector).Props.Expanded {
		t.Fatal("group did not collapse")
	}
	if reflect.DeepEqual(openIcon, app.Find(selector+"-icon").Drawing) {
		t.Fatal("folder icon did not reflect collapsed state")
	}
	app.Screenshot("project-folder-collapsed")
	app.Press(selector)
	app.WaitFor(child, time.Second)
	if !app.Find(selector).Props.Expanded {
		t.Fatal("group did not expand")
	}
	if nsgotest.Target() != "macos" {
		app.HoverOut()
		app.Hover(selector)
		app.Advance(450 * time.Millisecond)
		app.WaitFor("id:group-info-card", time.Second)
		app.Find("text:2 个会话")
		app.Screenshot("project-group-information-light")
		card := app.Find("id:group-info-card").ID
		app.Post(func() { s.cx.Post(func() { s.catalogueVersion++; s.changed() }) })
		app.Settle()
		if app.Find("id:group-info-card").ID != card {
			t.Fatal("catalogue refresh replaced group information")
		}
		app.HoverOut()
	}
	app.RightClick(selector)
	app.WaitFor("id:group-info-card", time.Second)
	app.Find("text:2 个会话")
	if !app.Find(selector).Props.Expanded {
		t.Fatal("secondary click collapsed the group")
	}
	app.Key("Escape")
	app.Press("id:theme-toggle")
	app.RightClick(selector)
	app.WaitFor("id:group-info-card", time.Second)
	app.Screenshot("project-group-information-dark")
}

func TestGroupInformationTouchHold(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("headless phone fixture supplies finger input")
	}
	var s *State
	var group directoryRow
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = sidebarInteractionFixture(cx)
		for _, r := range s.directoryRows() {
			if r.Kind == "project" {
				group = r
				break
			}
		}
		return s.directory(cx)
	}, nsgotest.Size(390, 900), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone, Locale: "zh-CN"}))
	app.LongPress("id:" + group.ID)
	app.WaitFor("id:group-info-card", time.Second)
	app.Find("text:2 个会话")
	if s.collapsed[group.Group] {
		t.Fatal("long press also collapsed the group")
	}
}
