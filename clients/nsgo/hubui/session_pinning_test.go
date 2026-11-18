package hubui

import (
	"fmt"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/clients/nsgo/hubui/locales"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestPinnedSessionsLeaveEveryProjectGrouping(t *testing.T) {
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := sidebarInteractionFixture(cx)
		for _, grouping := range []string{groupProject, groupDevice, groupAgent} {
			s.grouping = grouping
			for _, ss := range s.sessions {
				s.pinned[sessionIdentity(ss)] = 1
			}
			pinned, empty := 0, 0
			for _, row := range s.directoryRows() {
				if row.Kind == "device" || row.Kind == "agent" || row.Kind == "project" {
					t.Fatal("pinning left an empty project subtree", grouping, row.Kind)
				}
				if row.Kind == "empty" {
					empty++
					if row.Title != locales.For(cx).AllSessionsPinned() {
						t.Fatal("missing pinned-only explanation")
					}
				}
				if !row.Header {
					if row.Section != "pinned" {
						t.Fatal("pin duplicated outside Pinned", grouping, row.Section)
					}
					pinned++
				}
			}
			if pinned != 3 || empty != 2 {
				t.Fatalf("pinned=%d empty=%d", pinned, empty)
			}
			delete(s.pinned, sessionIdentity(s.sessions[0]))
			projects, recent := 0, 0
			for _, row := range s.directoryRows() {
				if row.Header {
					continue
				}
				if row.Section == "all" {
					projects++
				}
				if row.Section == "recent" {
					recent++
				}
			}
			if projects != 1 || recent != 1 {
				t.Fatal("unpin did not restore grouping and recency", grouping)
			}
		}
		return core.View{}
	})
}

func TestPinMovesSessionWithoutLosingDraftLight(t *testing.T) {
	testPinMovesSessionWithoutLosingDraft(t, false)
}
func TestPinMovesSessionWithoutLosingDraftDark(t *testing.T) {
	testPinMovesSessionWithoutLosingDraft(t, true)
}

func testPinMovesSessionWithoutLosingDraft(t *testing.T, dark bool) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if err := cx.Storage().SetItem("session-sidebar", `{"collapsed":{"section:pinned":true}}`); err != nil {
			t.Fatal(err)
		}
		s = workspaceLayoutFixture(cx)
		s.setAppearance(choose(dark, "dark", "light"))
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 960), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop, OS: core.OSMacOS}))
	build, design := s.sessions[0], s.sessions[2]
	pinnedBuild := "id:" + sessionRowID("pinned", build)
	pinnedDesign := "id:" + sessionRowID("pinned", design)
	recentBuild := "id:" + sessionRowID("recent", build)
	projectBuild := "id:" + sessionRowID("all", build)
	app.Find(pinnedDesign) // A saved collapsed section must start expanded.
	app.Type("id:chat-input", "保留当前草稿")
	input := app.Find("id:chat-input").ID
	checkDraft := func() {
		t.Helper()
		if app.Find("id:chat-input").ID != input || app.Find("id:chat-input").Text != "保留当前草稿" || str(s.selected, "id") != "design" {
			t.Fatal("moving a sidebar entry changed the open conversation or draft")
		}
	}
	app.Press("id:section:pinned")
	if app.Exists(pinnedDesign) {
		t.Fatal("manual collapse ignored")
	}
	app.RightClick(recentBuild)
	app.Press("id:session-pin-action")
	app.WaitFor(pinnedBuild, time.Second)
	app.Find(pinnedDesign)
	if app.Exists(recentBuild) || app.Exists(projectBuild) {
		t.Fatal("pin was duplicated in Recent/Projects")
	}
	checkDraft()
	app.Screenshot(fmt.Sprintf("pinned-exclusive-dark-%t", dark))
	app.RightClick(pinnedBuild)
	app.Press("id:session-pin-action")
	app.WaitFor(projectBuild, time.Second)
	app.Find(recentBuild)
	checkDraft()
	app.RightClick(pinnedDesign)
	app.Press("id:session-pin-action")
	app.WaitFor("id:"+sessionRowID("all", design), time.Second)
	if app.Exists("id:section:pinned") {
		t.Fatal("empty Pinned heading remained")
	}
	checkDraft()
}
