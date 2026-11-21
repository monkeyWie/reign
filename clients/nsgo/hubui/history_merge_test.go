package hubui

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestReorderedHistoryPreviewsDoNotAmplifyLiveTail(t *testing.T) {
	turn := func(id, status string) Object {
		return Object{"id": id, "status": status, "items": []Object{{"id": "answer", "role": "assistant", "text": id}}}
	}
	a, b, c, live := turn("a", "completed"), turn("b", "completed"), turn("c", "completed"), turn("live", "inProgress")
	old := Object{"turns": []Object{a, b, c, live}}
	// A refreshed history order can differ from the cache. Everything after
	// its last anchor is not necessarily missing from the received prefix.
	partial := Object{"turns": []Object{b, c, a}}
	for i := range 50 {
		preview := previewTranscript(old, partial)
		old = mergeLatest(old, preview) // State.setTranscript applies this again.
		turns := objects(old["turns"])
		if len(turns) != 4 {
			t.Fatalf("refresh %d grew four distinct turns into %d", i, len(turns))
		}
		for j, id := range []string{"b", "c", "a", "live"} {
			if str(turns[j], "id") != id {
				t.Fatalf("refresh %d changed history order at %d", i, j)
			}
		}
		if str(activeTurn(old), "id") != "live" {
			t.Fatal("refresh discarded the live tail")
		}
	}
	final := Object{"turns": []Object{b, c, a, turn("live", "completed")}}
	if got := mergeLatest(old, final); activeTurn(got) != nil || len(objects(got["turns"])) != 4 {
		t.Fatal("final history did not replace the live state")
	}
	if len(objects(partial["turns"])) != 3 || str(live, "status") != "inProgress" {
		t.Fatal("merge mutated a published snapshot")
	}
}

func TestDuplicateCachedHistoryConvergesAfterRestore(t *testing.T) {
	first := Object{"id": "history", "status": "completed"}
	stale := Object{"id": "live", "status": "inProgress", "items": []Object{{"id": "answer", "text": "prefix"}}}
	latest := Object{"id": "live", "status": "completed", "items": []Object{{"id": "answer", "text": "final answer"}}}
	cached := Object{"turns": []Object{first, stale, first, latest}, "resume": Object{"epoch": "saved", "cursor": 42}, "localMessages": []Object{{"id": "pending", "status": "uncertain", "text": "keep me"}}}
	for range 10 {
		cached = mergeLatest(nil, cached)
		turns := objects(cached["turns"])
		if len(turns) != 2 || str(turns[1], "status") != "completed" || str(objects(turns[1]["items"])[0], "text") != "final answer" {
			t.Fatal("restored duplicates survived or lost the latest content")
		}
		if number(object(cached["resume"]), "cursor") != 42 || len(objects(cached["localMessages"])) != 1 {
			t.Fatal("normalization lost cursor or outgoing delivery state")
		}
		data, err := json.Marshal(cached)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &cached); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShortRemoteBaselinePreservesCachedPrefixAndPagingCursor(t *testing.T) {
	var cached []Object
	for i := range 12 {
		cached = append(cached, Object{"id": fmt.Sprint(i), "status": "completed"})
	}
	old := Object{"streamEpoch": "fixture", "turns": cached, "nextCursor": "before-0"}
	current := Object{"streamEpoch": "fixture", "turns": cached[8:], "nextCursor": "before-8"}
	for range 3 {
		old = mergeLatest(old, current)
		if str(old, "nextCursor") != "before-0" || len(objects(old["turns"])) != 12 {
			t.Fatal("re-read or dropped verified older turns")
		}
	}
	current["streamEpoch"] = "other"
	latest := mergeLatest(old, current)
	if str(latest, "nextCursor") != "before-8" || len(objects(latest["turns"])) != 4 {
		t.Fatal("reused cursor across epochs")
	}
}

func TestItemPageRefreshJoinsAtExactMessage(t *testing.T) {
	item := func(id string) Object { return Object{"id": id, "role": "assistant", "text": id} }
	turn := func(items ...Object) Object { return Object{"id": "t", "status": "completed", "items": items} }
	old := Object{"streamEpoch": "x", "nextCursor": "older", "turns": []Object{turn(item("a"), item("b"))}}
	page := Object{"streamEpoch": "x", "nextCursor": "before-b", "partialTurn": "t", "turns": []Object{turn(item("b"), item("c"))}}
	got := mergeLatest(old, page)
	items := objects(objects(got["turns"])[0]["items"])
	if len(items) != 3 || str(items[0], "id") != "a" || str(got, "nextCursor") != "older" {
		t.Fatal(got)
	}
	page["turns"] = []Object{turn(item("z"))}
	got = mergeLatest(old, page)
	if len(objects(objects(got["turns"])[0]["items"])) != 1 || str(got, "nextCursor") != "before-b" {
		t.Fatal("bridged a gap", got)
	}
}
