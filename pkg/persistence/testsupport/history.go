// Package testsupport exercises the same persistence contract on SQLite and IndexedDB.
package testsupport

import (
	"context"
	"errors"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"testing"
)

func CheckHistory(t *testing.T, dao persistence.HistoryDAO) {
	t.Helper()
	ctx := context.Background()
	k := persistence.Session{Env: "fixture-" + persistence.Revision(), Provider: "demo", ID: "chat"}
	defer dao.ForgetHistory(ctx, k.Env)
	checkDirectionalPages(t, dao, k)
	for _, count := range []int{500, 2000, 5000} {
		k.ID = fmt.Sprint(count)
		items := make([]persistence.Object, count)
		for i := range items {
			items[i] = persistence.Object{"id": fmt.Sprint(i), "text": "first"}
		}
		history := func(items []persistence.Object, cursor int) persistence.Object {
			return persistence.Object{"turns": []persistence.Object{{"id": "turn", "items": items}}, "resume": persistence.Object{"cursor": cursor}}
		}
		old := history(items, 1)
		base := persistence.Diff("", nil, old)
		if err := dao.ApplyHistory(ctx, k, base); err != nil {
			t.Fatal(err)
		}
		next := append([]persistence.Object(nil), items...)
		next[count-1] = persistence.Object{"id": fmt.Sprint(count - 1), "text": "updated"}
		patch := persistence.Diff(base.Revision, old, history(next, 2))
		if len(patch.Messages) != 1 || len(patch.Turns) != 0 {
			t.Fatalf("%d message edits for one update", len(patch.Messages))
		}
		if err := dao.ApplyHistory(ctx, k, patch); err != nil {
			t.Fatal(err)
		}
		if err := dao.ApplyHistory(ctx, k, patch); !errors.Is(err, persistence.ErrConflict) {
			t.Fatal("accepted stale checkpoint", err)
		}
		appended := append(append([]persistence.Object{}, next...), persistence.Object{"id": "added", "text": "new"})
		add := persistence.Diff(patch.Revision, history(next, 2), history(appended, 3))
		if len(add.Messages) != 1 || len(add.Turns) != 0 {
			t.Fatal("append rewrote existing records")
		}
		if err := dao.ApplyHistory(ctx, k, add); err != nil {
			t.Fatal(err)
		}
		var before *persistence.PageCursor
		total := 0
		for {
			page, err := dao.ReadPage(ctx, k, persistence.PageQuery{Before: before})
			if err != nil {
				t.Fatal(err)
			}
			rows := persistence.Objects(persistence.Objects(page.Transcript["turns"])[0]["items"])
			if len(rows) == 0 || len(rows) > 50 {
				t.Fatal("invalid page", len(rows))
			}
			for j := len(rows) - 1; j >= 0; j-- {
				expected := fmt.Sprint(count - total)
				if total == 0 {
					expected = "added"
				}
				if rows[j]["id"] != expected {
					t.Fatalf("row %d: expected %s, got %v", total, expected, rows[j]["id"])
				}
				if total == 1 && rows[j]["text"] != "updated" {
					t.Fatal("lost update")
				}
				total++
			}
			before = page.Before
			if before == nil {
				break
			}
		}
		if total != count+1 {
			t.Fatal("incomplete history", total)
		}

	}
}

// Stored ordinals can be sparse or negative, including items in a later turn.
func checkDirectionalPages(t *testing.T, dao persistence.HistoryDAO, key persistence.Session) {
	t.Helper()
	ctx := context.Background()
	key.ID = "sparse"
	patch := persistence.HistoryPatch{Revision: persistence.Revision(), Head: persistence.Object{}}
	positions := []persistence.PageCursor{{Turn: -7, Item: -50}, {Turn: -7, Item: 9}, {Turn: 1}, {Turn: 4, Item: -12}, {Turn: 4, Item: 55}, {Turn: 20}}
	for i, at := range positions {
		turn := fmt.Sprint(at.Turn)
		if i == 0 || positions[i-1].Turn != at.Turn {
			patch.Turns = append(patch.Turns, persistence.Row{Turn: turn, Position: at.Turn, Value: persistence.Object{"id": turn}})
		}
		if at.Item != 0 {
			item := fmt.Sprint(at.Item)
			patch.Messages = append(patch.Messages, persistence.Row{Turn: turn, Item: item, Position: at.Item, Value: persistence.Object{"id": item}})
		}
	}
	if err := dao.ApplyHistory(ctx, key, patch); err != nil {
		t.Fatal(err)
	}
	first := persistence.PageCursor{Turn: -8}
	for _, upper := range []int{len(positions), 4} {
		q := persistence.PageQuery{After: &first, Limit: 2}
		if upper < len(positions) {
			q.Before = &positions[upper]
		}
		for i := 0; i < upper; i += 2 {
			p, err := dao.ReadPage(ctx, key, q)
			if err != nil || p.Start != positions[i] || p.End != positions[i+1] {
				t.Fatalf("forward [%v,%v], expected [%v,%v], err %v", p.Start, p.End, positions[i], positions[i+1], err)
			}
			if i+2 < upper && p.After == nil {
				t.Fatal("premature end of range")
			}
			q.After = &p.End
		}
		p, err := dao.ReadPage(ctx, key, q)
		if err != nil || len(persistence.Objects(p.Transcript["turns"])) != 0 || p.After != nil {
			t.Fatal("range did not end", p, err)
		}
	}
}

// Two independent connections compete for the same base revision. Forgetting
// the environment also rejects a delayed initial write after its rows are gone.
func CheckCompetingWriters(t *testing.T, a, b persistence.HistoryDAO) {
	t.Helper()
	ctx := context.Background()
	k := persistence.Session{Env: "race-" + persistence.Revision(), Provider: "fixture", ID: "same"}
	head := persistence.Object{"turns": []persistence.Object{{"id": "t", "items": []persistence.Object{{"id": "i", "text": "baseline"}}}}}
	first := persistence.Diff("", nil, head)
	if e := a.ApplyHistory(ctx, k, first); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 2)
	for i, dao := range []persistence.HistoryDAO{a, b} {
		next := persistence.Object{"turns": []persistence.Object{{"id": "t", "items": []persistence.Object{{"id": "i", "text": fmt.Sprint(i)}}}}}
		go func() { done <- dao.ApplyHistory(ctx, k, persistence.Diff(first.Revision, head, next)) }()
	}
	wins := 0
	for range 2 {
		e := <-done
		if e == nil {
			wins++
		} else if !errors.Is(e, persistence.ErrConflict) {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("competing commits", wins)
	}
	if e := b.ForgetHistory(ctx, k.Env); e != nil {
		t.Fatal(e)
	}
	if e := a.ApplyHistory(ctx, k, first); e == nil {
		t.Fatal("forgotten environment revived")
	}
}
