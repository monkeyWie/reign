package hubui

import (
	"fmt"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"slices"
	"testing"
	"time"
)

func TestRecentSectionStartsExpandedLight(t *testing.T) { testRecentSectionStartsExpanded(t, false) }
func TestRecentSectionStartsExpandedDark(t *testing.T)  { testRecentSectionStartsExpanded(t, true) }

func testRecentSectionStartsExpanded(t *testing.T, dark bool) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		if err := cx.Storage().SetItem("session-sidebar", `{"collapsed":{"section:recent":true,"section:all":true}}`); err != nil {
			t.Fatal(err)
		}
		s = sidebarInteractionFixture(cx)
		for i := range 5 {
			s.sessions = append(s.sessions, Object{"id": fmt.Sprint("finished-", i), "deviceId": "local", "providerId": "codex-app", "title": fmt.Sprintf("已完成的会话 %d", i+1), "createdAt": 90 - i, "recencyAt": (700 - i) * 1000, "updatedAt": 700 - i, "status": "completed"})
		}
		s.setAppearance(choose(dark, "dark", "light"))
		return s.directory(cx)
	}, nsgotest.Size(390, 980), nsgotest.Environment(core.Environment{Locale: "zh-CN", Idiom: core.IdiomDesktop, OS: core.OSMacOS}))
	recent := "id:" + sessionRowID("recent", s.sessions[0])
	app.Find(recent)
	app.Find("text:最近")
	for _, session := range s.sessions[3:] {
		app.Find("id:" + sessionRowID("recent", session))
	}
	if globalBounds(app.Find("id:section:all")).Origin.Y >= globalBounds(app.Find("id:section:recent")).Origin.Y {
		t.Fatal("Recent is not below Projects")
	}
	app.Press("id:section:all")
	app.Find(sessionSelector("local", "codex-app", "build"))
	app.Press("id:section:all")
	app.Find(recent)
	app.Screenshot(fmt.Sprintf("recent-default-expanded-dark-%t", dark))
	app.Press("id:section:recent")
	if app.Exists(recent) {
		t.Fatal("manual collapse did not hide Recent")
	}
	app.Press("id:section:pinned")
	app.HoverOut()
	for _, section := range []string{"pinned", "all", "recent"} {
		if app.Find("id:section:"+section).Props.Expanded || app.Find("id:section:"+section+"-disclosure").Style.Opacity < .99 {
			t.Fatal("collapsed section has no visible disclosure cue", section)
		}
	}
	app.Screenshot(fmt.Sprintf("sidebar-sections-collapsed-dark-%t", dark))
	app.Post(func() { s.cx.Post(func() { s.catalogueVersion++; s.changed() }) })
	app.Settle()
	if app.Exists(recent) {
		t.Fatal("refresh undid the user's manual collapse")
	}
	app.Press("id:section:recent")
	app.Find(recent)
	app.Post(func() {
		s.cx.Post(func() {
			s.sessions = nil
			s.catalogueVersion++
			s.changed()
		})
	})
	app.WaitFor("id:session-list-placeholder", time.Second)
	if app.Exists("id:section:recent") || app.Exists("id:section:all") {
		t.Fatal("empty directory repeated section placeholders")
	}
}

func TestRecentSessionsUnlimitedOrderingAndFilters(t *testing.T) {
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := newState(cx, newDemoAPI())
		s.sessions = []Object{
			{"id": "offline", "deviceId": "offline", "providerId": "pi", "recencyAt": 800000, "updatedAt": 800, "status": "idle"},
			{"id": "finished", "deviceId": "local", "providerId": "pi", "recencyAt": 700000, "updatedAt": 700, "status": "completed"},
			{"id": "new", "deviceId": "local", "providerId": "pi", "createdAt": 600},
			{"id": "tie-b", "deviceId": "local", "providerId": "pi", "recencyAt": 500000, "updatedAt": 500, "status": "running"},
			{"id": "tie-a", "deviceId": "local", "providerId": "pi", "recencyAt": 500000, "updatedAt": 500, "status": "waiting"},
			{"id": "older", "deviceId": "local", "providerId": "pi", "recencyAt": 400000, "updatedAt": 400, "status": "failed"},
			{"id": "unknown", "deviceId": "local", "providerId": "pi"},
		}
		s.pinned[sessionIdentity(s.sessions[0])] = 1
		check := func(want []string, wantAll int) {
			t.Helper()
			var got, sections []string
			all := 0
			for _, row := range s.directoryRows() {
				if row.Kind == "section" {
					sections = append(sections, row.Section)
				}
				if row.Header {
					continue
				}
				if row.Section == "recent" {
					if row.Depth != 0 {
						t.Error("Recent became a tree")
					}
					got = append(got, str(row.Session, "id"))
				}
				if row.Section == "all" {
					all++
				}
			}
			if !slices.Equal(got, want) || all != wantAll {
				t.Fatalf("Recent = %v, Projects = %d; want %v, %d", got, all, want, wantAll)
			}
			wantSections := []string{"pinned", "all", "recent"}
			if !slices.Equal(sections, wantSections) {
				t.Fatalf("section order = %v, want %v", sections, wantSections)
			}
		}
		check([]string{"finished", "new", "tie-a", "tie-b", "older", "unknown"}, 6)
		s.sessions[3]["status"] = "completed"
		check([]string{"finished", "new", "tie-a", "tie-b", "older", "unknown"}, 6)
		s.collapsed["section:all"] = true
		check([]string{"finished", "new", "tie-a", "tie-b", "older", "unknown"}, 0)
		delete(s.collapsed, "section:all")
		s.filters.Devices = map[string]bool{"local": true}
		check([]string{"finished", "new", "tie-a", "tie-b", "older", "unknown"}, 6)
		s.sessions[5]["recencyAt"] = 900000
		check([]string{"older", "finished", "new", "tie-a", "tie-b", "unknown"}, 6)
		s.removedSessions = map[string]bool{sessionIdentity(s.sessions[5]): true}
		check([]string{"finished", "new", "tie-a", "tie-b", "unknown"}, 5)
		return core.View{}
	})
}

func TestUnifiedTreeIdentityGroupingAndStableOrder(t *testing.T) {
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := newState(cx, newDemoAPI())
		s.devices = []Object{{"id": "local", "local": true, "connected": true}, {"id": "remote", "name": "Linux", "connected": true}, {"id": "offline", "connected": false}}
		s.sessions = []Object{
			{"deviceId": "local", "providerId": "codex-app", "id": "same", "title": "Local", "projectDir": "/mac/repo", "projectKey": "git:shared:.", "createdAt": 30, "recencyAt": 100000, "updatedAt": 100, "status": "running"},
			{"deviceId": "remote", "providerId": "codex-app", "id": "same", "title": "Remote", "projectDir": "/linux/repo", "projectKey": "git:shared:.", "createdAt": 20, "recencyAt": 200000, "updatedAt": 200, "status": "waiting"},
			{"deviceId": "remote", "providerId": "pi", "id": "third", "projectDir": "/linux/repo", "projectKey": "git:shared:.", "createdAt": 10, "recencyAt": 500000, "updatedAt": 500, "status": "idle"},
			{"deviceId": "offline", "providerId": "pi", "id": "old", "title": "Offline", "projectDir": "/linux/repo", "createdAt": 0, "recencyAt": 1000000, "updatedAt": 1000, "status": "running"},
		}
		s.pinned[sessionIdentity(s.sessions[0])] = 1
		check := func() {
			rows := s.directoryRows()
			counts := map[string]int{}
			ids := map[string]bool{}
			var all, recent []string
			for _, r := range rows {
				if ids[r.ID] {
					t.Error("duplicate row identity", r.ID)
				}
				ids[r.ID] = true
				if r.Header {
					continue
				}
				counts[r.Section]++
				if r.Section != "all" && r.Depth != 0 {
					t.Error("flat section became nested")
				}
				if r.Section == "all" {
					all = append(all, r.Title)
				}
				if r.Section == "recent" {
					recent = append(recent, r.Title)
				}
			}
			if counts["pinned"] != 1 || counts["recent"] != 3 || counts["all"] != 3 {
				t.Error("wrong membership", counts)
			}
			if len(recent) != 3 || recent[0] != "Offline" {
				t.Error("recent order", recent)
			}
			if len(all) != 3 || all[0] != "Remote" {
				t.Error("creation order changed with activity", all)
			}
		}
		check()
		root := s.sessionTree(uniqueSessions(s.sessions))
		if len(root) != 2 || root[0].Kind != "project" || root[0].Count != 3 || root[0].Target != "" {
			t.Error("project aggregation", root)
		}
		s.sessions[0]["updatedAt"] = 900 // activity cannot reorder All
		s.sessions[1]["updatedAt"] = 1000
		check()
		s.grouping = groupDevice
		root = s.sessionTree(uniqueSessions(s.sessions))
		if len(root) != 3 || root[0].Kind != "device" || root[0].Children[0].Kind != "project" {
			t.Fatal("device grouping")
		}
		s.grouping = groupAgent
		root = s.sessionTree(uniqueSessions(s.sessions))
		if root[0].Children[0].Kind != "agent" || root[0].Children[0].Children[0].Kind != "project" {
			t.Fatal("agent grouping")
		}
		// A shared path string is not enough to merge unrelated devices.
		a := copyObject(s.sessions[0])
		b := copyObject(a)
		delete(a, "projectKey")
		delete(b, "projectKey")
		b["deviceId"] = "remote"
		s.grouping = groupProject
		if len(s.sessionTree([]Object{a, b})) != 2 {
			t.Fatal("unrelated filesystem paths merged across devices")
		}
		return core.View{}
	})
}

func TestSidebarPreferencesPersistAndLiveStatusUpdates(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		s.devices = []Object{{"id": "local", "connected": true}}
		s.target = "local"
		ss := Object{"deviceId": "local", "providerId": "pi", "id": "one", "createdAt": 123, "status": "idle"}
		s.sessions = []Object{ss}
		s.selected = ss
		return button(cx, "save-sidebar", "Save", nil, "ghost", func() {
			s.togglePinned(ss)
			s.setGrouping(groupAgent)
			s.collapsed["section:all"] = true
			s.collapsed["section:recent"] = true
			s.collapsed["section:pinned"] = true
			s.sidebarHidden.Set(true)
			s.saveSidebarPreferences()
			next := newState(cx, newDemoAPI())
			if next.grouping != groupAgent || next.pinned[sessionIdentity(ss)] == 0 || !next.collapsed["section:all"] {
				t.Error("preferences not restored")
			}
			if !next.sidebarHidden.GetUntracked() {
				t.Error("sidebar visibility was not restored")
			}
			next.devices, next.sessions = s.devices, []Object{copyObject(ss)}
			next.sessions[0]["status"] = "running"
			pinnedVisible, recentEmpty := false, false
			for _, row := range next.directoryRows() {
				pinnedVisible = pinnedVisible || row.Section == "pinned" && !row.Header
				recentEmpty = recentEmpty || row.ID == "empty:recent"
			}
			if !pinnedVisible || !recentEmpty {
				t.Error("restored shortcuts remained collapsed or duplicated a pinned session")
			}
			s.setTranscript(Object{"turns": []Object{{"id": "turn", "status": "inProgress"}}})
			if !s.sessionActive(s.sessions[0]) || sessionCreated(s.sessions[0]) != 123000 {
				t.Error("live status did not show running work or changed creation time")
			}
			s.setTranscript(Object{"turns": []Object{{"id": "turn", "status": "completed"}}})
			if s.sessionActive(s.sessions[0]) {
				t.Error("completed turn retained a running status")
			}
		})
	})
	app.Press("id:save-sidebar")
	app.Advance(time.Millisecond)
}

func projectGroups(sessions []Object) []*sessionTreeNode {
	return (&State{grouping: groupProject}).sessionTree(uniqueSessions(sessions))
}
