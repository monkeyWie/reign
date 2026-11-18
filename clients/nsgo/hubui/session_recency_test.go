package hubui

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestRecentOrderChangesOnlyAtMessageBoundaries(t *testing.T) {
	nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s := newState(cx, newDemoAPI())
		s.target = "local"
		const start int64 = 1794960000000
		running := Object{"id": "running", "providerId": "pi", "createdAt": start, "recencyAt": start + 1000, "updatedAt": start + 1000, "status": "running"}
		stopped := Object{"id": "stopped", "providerId": "pi", "createdAt": start, "recencyAt": start + 2000, "updatedAt": start + 2000, "status": "completed"}
		s.applyEnvironmentPage("local", "pi", []Object{running, stopped}, true)
		s.selected = running
		s.transcript = Object{"session": copyObject(running), "turns": []Object{{"id": "turn", "status": "inProgress", "recencyAt": start + 1000, "items": []Object{}}}}
		check := func(want ...string) {
			t.Helper()
			var got []string
			for _, row := range s.directoryRows() {
				if row.Section == "recent" && !row.Header {
					got = append(got, str(row.Session, "id"))
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("Recent = %v, want %v", got, want)
			}
		}
		check("stopped", "running")
		for i := range 3 {
			poll := copyObject(running)
			poll["updatedAt"] = start + 3000 + int64(i)*1000
			s.applyEnvironmentPage("local", "pi", []Object{poll, stopped}, true)
			check("stopped", "running")
			s.transcript, _ = applyEvents(s.transcript, []Object{{"kind": "text", "turnId": "turn", "itemId": "answer", "delta": "progress", "offset": i * 8}})
			s.updateSelectedSummary()
			check("stopped", "running")
		}
		// A sent follow-up advances recency once; stale snapshots cannot undo it.
		s.transcript, _ = applyEvents(s.transcript, []Object{{"kind": "item", "turnId": "turn", "itemId": "user", "item": Object{"id": "user", "role": "user", "timestampMs": start + 6000}}})
		s.updateSelectedSummary()
		check("running", "stopped")
		s.applyEnvironmentPage("local", "pi", []Object{running, stopped}, true)
		check("running", "stopped")
		stopped = copyObject(stopped)
		stopped["recencyAt"] = start + 7000
		s.applyEnvironmentPage("local", "pi", []Object{running, stopped}, true)
		check("stopped", "running")
		// Final completion advances once, independently of delta arrival time.
		s.transcript, _ = applyEvents(s.transcript, []Object{{"kind": "turn", "turnId": "turn", "status": "completed", "recencyAt": start + 8000}})
		s.updateSelectedSummary()
		check("running", "stopped")
		s.applyEnvironmentPage("local", "pi", []Object{running, stopped}, true)
		s.updateSelectedSummary()
		check("running", "stopped")
		return core.View{}
	})
}
