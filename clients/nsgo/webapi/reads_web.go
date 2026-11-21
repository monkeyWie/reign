//go:build js && wasm

package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"syscall/js"
)

func (s *Store) SaveList(ctx context.Context, k persistence.Session, kind persistence.ReadKind, list []Object) error {
	table := kind.Table()
	if table == "" {
		return errors.New("unknown read model")
	}
	values := make([]string, len(list))
	for i, v := range list {
		b, err := json.Marshal(kind.Record(v))
		if err != nil {
			return err
		}
		values[i] = string(b)
	}
	return s.envWrite(ctx, k.Env, []any{"read_state", table}, func(tx js.Value) func() {
		store := tx.Call("objectStore", table)
		r := store.Call("getAll", prefixRange(scopeKey(k)))
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			old := r.Get("result")
			for i, v := range values {
				if i >= old.Length() || old.Index(i).String() != v {
					store.Call("put", v, append(scopeKey(k), i))
				}
			}
			for i := len(values); i < old.Length(); i++ {
				store.Call("delete", append(scopeKey(k), i))
			}
			tx.Call("objectStore", "read_state").Call("put", true, append(scopeKey(k), string(kind)))
			return nil
		})
		r.Set("onsuccess", cb)
		return cb.Release
	})
}
func (s *Store) LoadList(ctx context.Context, k persistence.Session, kind persistence.ReadKind) ([]Object, error) {
	table := kind.Table()
	if table == "" {
		return nil, nil
	}
	var head, rows js.Value
	err := s.transaction(ctx, []any{"read_state", table}, "readonly", func(tx js.Value) func() {
		head = tx.Call("objectStore", "read_state").Call("get", append(scopeKey(k), string(kind)))
		rows = tx.Call("objectStore", table).Call("getAll", prefixRange(scopeKey(k)))
		return nil
	})
	if err != nil || !head.Get("result").Truthy() {
		return nil, err
	}
	a := rows.Get("result")
	out := make([]Object, a.Length())
	for i := range out {
		if err = json.Unmarshal([]byte(a.Index(i).String()), &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
