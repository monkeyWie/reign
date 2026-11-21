//go:build js && wasm

package webapi

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"syscall/js"
	"time"
)

// Binary content is separate from the eviction index, so evicting a large
// entry does not copy it into the WASM heap. Both stores change atomically.
func (s *Store) Download(ctx context.Context, method, env string, p Object) (any, error) {
	key := []any{env, str(p, "key")}
	tables := []any{"download_cache", "download_sizes"}
	var req js.Value
	if method == "client.assetCache.clear" {
		err := s.transaction(ctx, tables, "readwrite", func(tx js.Value) func() {
			for _, t := range tables {
				st := tx.Call("objectStore", t)
				if env == "" {
					st.Call("clear")
				} else {
					st.Call("delete", prefixRange([]any{env}))
				}
			}
			return nil
		})
		return nil, err
	}
	var data []byte
	var err error
	if method == "client.assetCache.put" {
		data, err = base64.StdEncoding.DecodeString(str(p, "data"))
		if err != nil || len(data) > 50<<20 {
			return nil, errors.New("附件超过缓存上限或格式无效")
		}
	}
	now := float64(time.Now().UnixMilli())
	err = s.envWrite(ctx, env, tables, func(tx js.Value) func() {
		blobs := tx.Call("objectStore", "download_cache")
		index := tx.Call("objectStore", "download_sizes")
		if method == "client.assetCache.get" {
			req = blobs.Call("get", key)
			cb := js.FuncOf(func(js.Value, []js.Value) any {
				if v := req.Get("result"); v.Truthy() {
					index.Call("put", map[string]any{"env": env, "key": key[1], "size": v.Length(), "touched": now}, key)
				}
				return nil
			})
			req.Set("onsuccess", cb)
			return cb.Release
		}
		r := index.Call("getAll")
		cb := js.FuncOf(func(js.Value, []js.Value) any {
			arr := r.Get("result")
			var entries []js.Value
			total := len(data)
			for i := 0; i < arr.Length(); i++ {
				v := arr.Index(i)
				if v.Get("env").String() != env || v.Get("key").String() != key[1] {
					entries = append(entries, v)
					total += v.Get("size").Int()
				}
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].Get("touched").Float() < entries[j].Get("touched").Float() })
			for _, v := range entries {
				if total <= 96<<20 {
					break
				}
				victim := []any{v.Get("env").String(), v.Get("key").String()}
				blobs.Call("delete", victim)
				index.Call("delete", victim)
				total -= v.Get("size").Int()
			}
			bytes := js.Global().Get("Uint8Array").New(len(data))
			js.CopyBytesToJS(bytes, data)
			blobs.Call("put", bytes, key)
			index.Call("put", map[string]any{"env": env, "key": key[1], "size": len(data), "touched": now}, key)
			return nil
		})
		r.Set("onsuccess", cb)
		return cb.Release
	})
	if err != nil || method != "client.assetCache.get" {
		return nil, err
	}
	v := req.Get("result")
	if !v.Truthy() {
		return nil, nil
	}
	data = make([]byte, v.Length())
	js.CopyBytesToGo(data, v)
	return Object{"data": base64.StdEncoding.EncodeToString(data)}, nil
}
