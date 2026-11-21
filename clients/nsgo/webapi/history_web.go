//go:build js && wasm

package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"syscall/js"

	"github.com/monkeyWie/reignhub/pkg/persistence"
)

var historyTables = []any{"history_heads", "turns", "messages", "outgoing_messages", "outgoing_matches", "queued_messages", "message_anchors"}

func scopeKey(k persistence.Session) []any { return []any{k.Env, k.Provider, k.ID} }
func prefixRange(prefix []any) js.Value {
	upper := append(append([]any{}, prefix...), []any{})
	return js.Global().Get("IDBKeyRange").Call("bound", prefix, upper, false, true)
}
func (s *Store) transaction(ctx context.Context, tables []any, mode string, issue func(js.Value) func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	err := s.open()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	tx := s.db.Call("transaction", tables, mode)
	s.mu.Unlock()
	done := make(chan error, 1)
	complete := js.FuncOf(func(js.Value, []js.Value) any { done <- nil; return nil })
	aborted := js.FuncOf(func(js.Value, []js.Value) any { done <- errors.New("IndexedDB transaction aborted"); return nil })
	tx.Set("oncomplete", complete)
	tx.Set("onabort", aborted)
	cleanup := issue(tx)
	select {
	case err = <-done:
	case <-ctx.Done():
		// abort can race with an already committed transaction.
		func() { defer func() { recover() }(); tx.Call("abort") }()
		err = <-done
		if err != nil {
			err = ctx.Err()
		}
	}
	tx.Set("oncomplete", js.Null())
	tx.Set("onabort", js.Null())
	complete.Release()
	aborted.Release()
	if cleanup != nil {
		cleanup()
	}
	return err
}

type encodedRow struct {
	key    []any
	data   map[string]any
	turn   bool
	anchor any
}

func (s *Store) ApplyHistory(ctx context.Context, k persistence.Session, p persistence.HistoryPatch) error {
	if err := p.Validate(k); err != nil {
		return err
	}
	head, err := json.Marshal(p.Head)
	if err != nil {
		return err
	}
	key := scopeKey(k)
	var rows []encodedRow
	for _, r := range p.Turns {
		op := encodedRow{key: append(scopeKey(k), r.Turn), turn: true}
		if r.Value != nil {
			b, e := json.Marshal(r.Value)
			if e != nil {
				return e
			}
			op.data = map[string]any{"order": append(scopeKey(k), r.Position), "turn": r.Turn, "position": r.Position, "json": string(b)}
		}
		rows = append(rows, op)
	}
	for _, r := range p.Messages {
		op := encodedRow{key: append(scopeKey(k), r.Turn, r.Item)}
		if r.Value != nil {
			b, e := json.Marshal(r.Value)
			if e != nil {
				return e
			}
			op.data = map[string]any{"order": append(scopeKey(k), r.Turn, r.Position), "turn": r.Turn, "position": r.Position, "json": string(b)}
			if r.Value["role"] == "user" {
				id, _ := r.Value["id"].(string)
				body, _ := r.Value["text"].(string)
				op.anchor = map[string]any{"turn": r.Turn, "position": r.Position, "id": strings.TrimPrefix(r.Turn, "id:") + "|" + id, "preview": persistence.Snippet(body)}
			}

		}
		rows = append(rows, op)
	}
	type side struct {
		key   []any
		table string
		value any
	}
	var sideRows []side
	for _, v := range p.Overlay {
		if !slices.Contains(persistence.OverlayTables, v.Table) {
			return errors.New("invalid overlay table")
		}
		row := side{key: append(scopeKey(k), v.ID, v.Ref), table: v.Table}
		if v.Value != nil {
			b, e := json.Marshal(v.Value)
			if e != nil {
				return e
			}
			row.value = map[string]any{"id": v.ID, "position": v.Position, "json": string(b)}
		}
		sideRows = append(sideRows, row)
	}
	var conflict bool
	err = s.envWrite(ctx, k.Env, historyTables, func(tx js.Value) func() {
		heads := tx.Call("objectStore", "history_heads")
		turns := tx.Call("objectStore", "turns")
		items := tx.Call("objectStore", "messages")
		r := heads.Call("get", key)
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			prev := r.Get("result")
			revision := ""
			if prev.Truthy() {
				revision = prev.Get("revision").String()
			}
			if revision != p.Base {
				conflict = true
				tx.Call("abort")
				return nil
			}
			if p.Replace && p.Base != "" {
				span := prefixRange(key)
				for _, table := range persistence.OverlayTables {
					if p.PreserveOutgoing && table != persistence.Queued {
						continue
					}
					tx.Call("objectStore", table).Call("delete", span)
				}
				turns.Call("delete", span)
				items.Call("delete", span)
				tx.Call("objectStore", "message_anchors").Call("delete", span)
			}
			for _, op := range rows {
				if !op.turn {
					anchors := tx.Call("objectStore", "message_anchors")
					if op.anchor == nil {
						anchors.Call("delete", op.key)
					} else {
						anchors.Call("put", op.anchor, op.key)
					}
				}
				table := items
				if op.turn {
					table = turns
				}
				if op.data == nil {
					table.Call("delete", op.key)
					if op.turn {
						items.Call("delete", prefixRange(op.key))
						tx.Call("objectStore", "message_anchors").Call("delete", prefixRange(op.key))
					}
				} else {
					table.Call("put", op.data, op.key)
				}
			}
			for _, r := range sideRows {
				table := tx.Call("objectStore", r.table)
				if r.value == nil {
					table.Call("delete", r.key)
				} else {
					table.Call("put", r.value, r.key)
				}
			}
			heads.Call("put", map[string]any{"revision": p.Revision, "json": string(head)}, key)
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
func (s *Store) ForgetHistory(ctx context.Context, target string) error {
	tables := append(append([]any{}, historyTables...), "sessions", "directory_heads", "providers", "models", "approval_previews", "read_state", "draft_files", "draft_blobs", "drafts")
	return s.transaction(ctx, append(append([]any{}, tables...), "forgotten", "environments"), "readwrite", func(tx js.Value) func() {
		tx.Call("objectStore", "forgotten").Call("put", true, target)
		tx.Call("objectStore", "environments").Call("delete", target)
		span := prefixRange([]any{target})
		for _, name := range tables {
			tx.Call("objectStore", name).Call("delete", span)
		}
		return nil
	})
}

// The check and writes share the same transaction with ForgetHistory across tabs.
func (s *Store) envWrite(ctx context.Context, env string, tables []any, issue func(js.Value) func()) error {
	return s.transaction(ctx, append(append([]any{}, tables...), "forgotten"), "readwrite", func(tx js.Value) func() {
		var release func()
		r := tx.Call("objectStore", "forgotten").Call("get", env)
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			if r.Get("result").Truthy() {
				tx.Call("abort")
				return nil
			}
			release = issue(tx)
			return nil
		})
		r.Set("onsuccess", cb)
		return func() {
			cb.Release()
			if release != nil {
				release()
			}
		}
	})
}
