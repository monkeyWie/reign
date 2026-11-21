//go:build js && wasm

package webapi

import (
	"context"
	"encoding/json"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"syscall/js"
)

func (s *Store) Environments() ([]Object, error) {
	var req js.Value
	err := s.transaction(context.Background(), []any{"environments"}, "readonly", func(tx js.Value) func() { req = tx.Call("objectStore", "environments").Call("getAll"); return nil })
	if err != nil {
		return nil, err
	}
	a := req.Get("result")
	out := make([]Object, a.Length())
	for i := range out {
		if err = json.Unmarshal([]byte(a.Index(i).String()), &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Store) SaveEnvironment(ctx context.Context, value Object) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	id := str(value, "id")
	return s.envWrite(ctx, id, []any{"environments"}, func(tx js.Value) func() {
		tx.Call("objectStore", "environments").Call("put", string(b), id)
		return nil
	})
}
func (s *Store) Route(ctx context.Context, peer string) (string, error) {
	var id string
	err := s.transaction(ctx, []any{"environment_routes", "forgotten"}, "readwrite", func(tx js.Value) func() {
		routes := tx.Call("objectStore", "environment_routes")
		r := routes.Call("get", peer)
		var check js.Value
		retired := js.FuncOf(func(js.Value, []js.Value) any {
			if check.Get("result").Truthy() {
				id = peer + "|" + persistence.Revision()
			}
			routes.Call("put", id, peer)
			return nil
		})
		found := js.FuncOf(func(js.Value, []js.Value) any {
			id = peer
			if v := r.Get("result"); v.Truthy() {
				id = v.String()
			}
			check = tx.Call("objectStore", "forgotten").Call("get", id)
			check.Set("onsuccess", retired)
			return nil
		})
		r.Set("onsuccess", found)
		return func() { found.Release(); retired.Release() }
	})
	return id, err
}
func (s *Store) LoadIdentity(ctx context.Context, service string) (Object, error) {
	var req js.Value
	err := s.transaction(ctx, []any{"identities"}, "readonly", func(tx js.Value) func() {
		req = tx.Call("objectStore", "identities").Call("get", []any{service, "controller"})
		return nil
	})
	if err != nil || !req.Get("result").Truthy() {
		return nil, err
	}
	var out Object
	err = json.Unmarshal([]byte(req.Get("result").String()), &out)
	return out, err
}
func (s *Store) SaveIdentity(ctx context.Context, service string, v Object) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.transaction(ctx, []any{"identities"}, "readwrite", func(tx js.Value) func() {
		tx.Call("objectStore", "identities").Call("put", string(raw), []any{service, "controller"})
		return nil
	})
}
