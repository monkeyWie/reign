//go:build js && wasm

package webapi

import (
	"context"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"sort"
	"syscall/js"
)

func (s *Store) Navigation(ctx context.Context, k persistence.Session) ([]persistence.Landmark, error) {
	var anchors js.Value
	requests := map[string]js.Value{}
	err := s.transaction(ctx, []any{"message_anchors", "turns"}, "readonly", func(tx js.Value) func() {
		anchors = tx.Call("objectStore", "message_anchors").Call("getAll", prefixRange(scopeKey(k)))
		st := tx.Call("objectStore", "turns")
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			a := anchors.Get("result")
			for i := 0; i < a.Length(); i++ {
				id := a.Index(i).Get("turn").String()
				if _, ok := requests[id]; !ok {
					requests[id] = st.Call("get", append(scopeKey(k), id))
				}
			}
			return nil
		})
		anchors.Set("onsuccess", cb)
		return cb.Release
	})
	if err != nil {
		return nil, err
	}
	a := anchors.Get("result")
	var all []persistence.Landmark
	for i := 0; i < a.Length(); i++ {
		v := a.Index(i)
		t := requests[v.Get("turn").String()].Get("result")
		if !t.Truthy() {
			continue
		}
		all = append(all, persistence.Landmark{ID: v.Get("id").String(), Preview: v.Get("preview").String(), Position: persistence.PageCursor{Turn: t.Get("position").Int(), Item: v.Get("position").Int()}})
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].Position, all[j].Position
		return a.Turn < b.Turn || a.Turn == b.Turn && a.Item < b.Item
	})
	out := []persistence.Landmark{}
	for _, i := range persistence.Samples(len(all), 24) {
		out = append(out, all[i])
	}
	return out, nil
}
