//go:build js && wasm

package webapi

import (
	"context"
	"errors"
	"github.com/monkeyWie/reignhub/pkg/composerdraft"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"syscall/js"
)

var draftTables = []any{"drafts", "draft_files", "draft_blobs"}

func (s *Store) ReadDraft(ctx context.Context, k persistence.Session) (composerdraft.Draft, error) {
	var d composerdraft.Draft
	var head, files js.Value
	var blobs []js.Value
	err := s.transaction(ctx, draftTables, "readonly", func(tx js.Value) func() {
		key := scopeKey(k)
		head = tx.Call("objectStore", "drafts").Call("get", key)
		files = tx.Call("objectStore", "draft_files").Call("getAll", prefixRange(key))
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			a := files.Get("result")
			for i := 0; i < a.Length(); i++ {
				f := a.Index(i)
				hash := f.Get("hash").String()
				d.Files = append(d.Files, composerdraft.File{Name: f.Get("name").String(), MIME: f.Get("mime").String(), Hash: hash, Size: f.Get("size").Int()})
				blobs = append(blobs, tx.Call("objectStore", "draft_blobs").Call("get", append(scopeKey(k), hash)))
			}
			return nil
		})
		files.Set("onsuccess", cb)
		return cb.Release
	})
	if err != nil {
		return d, err
	}
	h := head.Get("result")
	if !h.Truthy() {
		return d, nil
	}
	d.Text = h.Get("text").String()
	d.Model = h.Get("model").String()
	d.ModelBase = h.Get("base").String()
	for i, r := range blobs {
		v := r.Get("result")
		f := &d.Files[i]
		if !v.Truthy() || v.Length() != f.Size {
			return d, errors.New("missing draft attachment")
		}
		f.Data = make([]byte, f.Size)
		js.CopyBytesToGo(f.Data, v)
		if composerdraft.Hash(f.Data) != f.Hash {
			return d, errors.New("draft attachment integrity check failed")
		}
	}
	return d, nil
}
func (s *Store) SaveDraft(ctx context.Context, k persistence.Session, d composerdraft.Draft) error {
	key := scopeKey(k)
	var invalid bool
	err := s.envWrite(ctx, k.Env, draftTables, func(tx js.Value) func() {
		heads := tx.Call("objectStore", "drafts")
		files := tx.Call("objectStore", "draft_files")
		blobs := tx.Call("objectStore", "draft_blobs")
		old := blobs.Call("getAllKeys", prefixRange(key))
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			existing := map[string]bool{}
			a := old.Get("result")
			for i := 0; i < a.Length(); i++ {
				existing[a.Index(i).Index(3).String()] = true
			}
			keep := map[string]bool{}
			for i, f := range d.Files {
				keep[f.Hash] = true
				if !existing[f.Hash] {
					if f.Data == nil && f.Size > 0 {
						invalid = true
						tx.Call("abort")
						return nil
					}
					v := js.Global().Get("Uint8Array").New(len(f.Data))
					js.CopyBytesToJS(v, f.Data)
					blobs.Call("put", v, append(scopeKey(k), f.Hash))
					existing[f.Hash] = true
				}
				files.Call("put", map[string]any{"name": f.Name, "mime": f.MIME, "hash": f.Hash, "size": f.Size}, append(scopeKey(k), i))
			}
			for hash := range existing {
				if !keep[hash] {
					blobs.Call("delete", append(scopeKey(k), hash))
				}
			}
			files.Call("delete", js.Global().Get("IDBKeyRange").Call("bound", append(scopeKey(k), len(d.Files)), append(scopeKey(k), []any{})))
			if d.Text == "" && d.Model == "" && len(d.Files) == 0 {
				heads.Call("delete", key)
			} else {
				heads.Call("put", map[string]any{"text": d.Text, "model": d.Model, "base": d.ModelBase}, key)
			}
			return nil
		})
		old.Set("onsuccess", cb)
		return cb.Release
	})
	if invalid {
		return errors.New("missing draft attachment")
	}
	return err
}
