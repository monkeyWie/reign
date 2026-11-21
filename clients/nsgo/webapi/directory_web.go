//go:build js && wasm

package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"strconv"
	"syscall/js"
)

func dirHead(v js.Value) persistence.DirectoryHead {
	if !v.Truthy() {
		return persistence.DirectoryHead{}
	}
	cursor, _ := strconv.ParseInt(v.Get("cursor").String(), 10, 64)
	return persistence.DirectoryHead{Epoch: v.Get("epoch").String(), Cursor: cursor}
}
func (s *Store) DirectoryPosition(ctx context.Context, k persistence.DirectoryKey) (persistence.DirectoryHead, error) {
	var r js.Value
	err := s.transaction(ctx, []any{"directory_heads"}, "readonly", func(tx js.Value) func() {
		r = tx.Call("objectStore", "directory_heads").Call("get", []any{k.Env, k.Provider})
		return nil
	})
	if err != nil {
		return persistence.DirectoryHead{}, err
	}
	return dirHead(r.Get("result")), nil
}
func (s *Store) WriteDirectory(ctx context.Context, k persistence.DirectoryKey, old persistence.DirectoryHead, b persistence.DirectoryBatch) error {
	values := make([]string, len(b.Upserts))
	for i, v := range b.Upserts {
		if str(v, "id") == "" {
			return fmt.Errorf("empty session id")
		}
		raw, e := json.Marshal(v)
		if e != nil {
			return e
		}
		values[i] = string(raw)
	}
	key := []any{k.Env, k.Provider}
	var conflict bool
	err := s.envWrite(ctx, k.Env, []any{"sessions", "directory_heads"}, func(tx js.Value) func() {
		heads := tx.Call("objectStore", "directory_heads")
		sessions := tx.Call("objectStore", "sessions")
		r := heads.Call("get", key)
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			v := r.Get("result")
			if b.Preview && v.Truthy() {
				return nil
			}
			if dirHead(v) != old {
				conflict = true
				tx.Call("abort")
				return nil
			}
			if b.Reset {
				sessions.Call("delete", prefixRange(key))
			}
			for i, v := range b.Upserts {
				sessions.Call("put", values[i], []any{k.Env, k.Provider, str(v, "id")})
			}
			for _, id := range b.Deletes {
				sessions.Call("delete", []any{k.Env, k.Provider, id})
			}
			if !b.Preview {
				heads.Call("put", map[string]any{"epoch": b.Head.Epoch, "cursor": strconv.FormatInt(b.Head.Cursor, 10)}, key)
			}
			return nil
		})
		r.Set("onsuccess", cb)
		return cb.Release
	})
	if conflict {
		return persistence.ErrConflict
	}
	return err
}
func (s *Store) ListSessions(ctx context.Context, k persistence.DirectoryKey) ([]Object, error) {
	key := []any{k.Env}
	if k.Provider != "" {
		key = append(key, k.Provider)
	}
	var r js.Value
	err := s.transaction(ctx, []any{"sessions"}, "readonly", func(tx js.Value) func() {
		r = tx.Call("objectStore", "sessions").Call("getAll", prefixRange(key))
		return nil
	})
	if err != nil {
		return nil, err
	}
	a := r.Get("result")
	out := make([]Object, a.Length())
	for i := range out {
		if err = json.Unmarshal([]byte(a.Index(i).String()), &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
