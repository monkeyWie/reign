//go:build js && wasm

package webapi

import (
	"context"
	"encoding/json"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"sort"
	"syscall/js"
)

func (s *Store) ReadPage(ctx context.Context, k persistence.Session, q persistence.PageQuery) (persistence.HistoryPage, error) {
	var b *persistence.PageBuilder
	var side []js.Value
	var failure error
	key := scopeKey(k)
	bound, direction := q.Before, "prev"
	if q.After != nil {
		bound, direction = q.After, "next"
	}
	span := func(prefix []any, ordinal int, exclusive bool) js.Value {
		at := append(append([]any{}, prefix...), ordinal)
		if q.After != nil {
			return js.Global().Get("IDBKeyRange").Call("bound", at, append(prefix, []any{}), exclusive, true)
		}
		return js.Global().Get("IDBKeyRange").Call("bound", prefix, at, false, exclusive)
	}
	err := s.transaction(ctx, historyTables, "readonly", func(tx js.Value) func() {
		if bound == nil {
			for _, table := range persistence.OverlayTables {
				side = append(side, tx.Call("objectStore", table).Call("getAll", prefixRange(key)))
			}
		}
		head := tx.Call("objectStore", "history_heads").Call("get", key)
		var turns, items js.Value
		var turnPos int
		itemCB := js.FuncOf(func(js.Value, []js.Value) any {
			c := items.Get("result")
			if !c.Truthy() {
				b.EndTurn(bound != nil && turnPos == bound.Turn)
				turns.Get("result").Call("continue")
				return nil
			}
			v := c.Get("value")
			ok, e := b.Item(v.Get("position").Int(), []byte(v.Get("json").String()))
			if e != nil {
				failure = e
				tx.Call("abort")
			} else if ok {
				c.Call("continue")
			}
			return nil
		})
		turnCB := js.FuncOf(func(js.Value, []js.Value) any {
			c := turns.Get("result")
			if !c.Truthy() {
				return nil
			}
			v := c.Get("value")
			turnPos = v.Get("position").Int()
			ok, e := b.Turn(turnPos, []byte(v.Get("json").String()))
			if e != nil {
				failure = e
				tx.Call("abort")
				return nil
			}
			if !ok {
				return nil
			}
			prefix := append(scopeKey(k), v.Get("turn").String())
			r := prefixRange(prefix)
			if bound != nil && turnPos == bound.Turn {
				r = span(prefix, bound.Item, true)
			}
			items = tx.Call("objectStore", "messages").Call("index", "ordered").Call("openCursor", r, direction)
			items.Set("onsuccess", itemCB)
			return nil
		})
		headCB := js.FuncOf(func(js.Value, []js.Value) any {
			h := head.Get("result")
			if !h.Truthy() {
				return nil
			}
			var meta Object
			if e := json.Unmarshal([]byte(h.Get("json").String()), &meta); e != nil {
				failure = e
				tx.Call("abort")
				return nil
			}
			b = persistence.NewPage(meta, h.Get("revision").String(), q)
			r := prefixRange(key)
			if bound != nil {
				r = span(key, bound.Turn, false)
			}
			turns = tx.Call("objectStore", "turns").Call("index", "ordered").Call("openCursor", r, direction)
			turns.Set("onsuccess", turnCB)
			return nil
		})
		head.Set("onsuccess", headCB)
		return func() { headCB.Release(); turnCB.Release(); itemCB.Release() }
	})
	if failure != nil {
		err = failure
	}
	if err != nil || b == nil {
		return persistence.HistoryPage{}, err
	}
	p := b.Finish()
	var all []persistence.OverlayRow
	for i, req := range side {
		a := req.Get("result")
		for j := 0; j < a.Length(); j++ {
			v := a.Index(j)
			r := persistence.OverlayRow{Table: persistence.OverlayTables[i], ID: v.Get("id").String(), Position: v.Get("position").Int()}
			if err = json.Unmarshal([]byte(v.Get("json").String()), &r.Value); err != nil {
				return persistence.HistoryPage{}, err
			}
			all = append(all, r)
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Position < all[j].Position })
	persistence.RestoreOverlay(p.Transcript, all)
	return p, nil
}
