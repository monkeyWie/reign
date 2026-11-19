// Package composerdraft stores controller-owned drafts, never Agent commands.
package composerdraft

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type Store interface {
	Get(string) (any, error)
	Put(string, any) error
	Delete(string) error
	Keys() ([]string, error)
}

type File struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Hash string `json:"hash"`
	Size int    `json:"size"`
	Data []byte `json:"data,omitempty"`
}
type Draft struct {
	Text  string `json:"text"`
	Model string `json:"model"`
	Files []File `json:"files"`
}

func Prefix(target string) string {
	return "draft:" + base64.RawURLEncoding.EncodeToString([]byte(target)) + ":"
}
func Key(target, provider, session string) string {
	b, _ := json.Marshal([]string{provider, session})
	return Prefix(target) + base64.RawURLEncoding.EncodeToString(b)
}
func Hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func decode(v any, out any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}

// Call is local-only on both native SQLite and browser IndexedDB. Immutable
// file bytes are written before the draft that references them. Text edits do
// not rewrite attachments, and draft bytes never enter the evictable cache.
func Call(ctx context.Context, store Store, method, target string, p map[string]any) (any, error) {
	provider, _ := p["provider"].(string)
	session, _ := p["session"].(string)
	if target == "" || provider == "" || session == "" {
		return nil, fmt.Errorf("invalid draft identity")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	key := Key(target, provider, session)
	v, e := store.Get(key)
	if e != nil {
		return nil, e
	}
	var old Draft
	if v != nil {
		if e = decode(v, &old); e != nil {
			return nil, e
		}
	}
	if method == "client.draft.get" {
		for i := range old.Files {
			if e = ctx.Err(); e != nil {
				return nil, e
			}
			v, e = store.Get(key + ":" + old.Files[i].Hash)
			if e != nil {
				return nil, e
			}
			if v == nil {
				return nil, fmt.Errorf("draft attachment is unavailable")
			}
			if e = decode(v, &old.Files[i].Data); e != nil {
				return nil, e
			}
			if len(old.Files[i].Data) != old.Files[i].Size || Hash(old.Files[i].Data) != old.Files[i].Hash {
				return nil, fmt.Errorf("draft attachment integrity check failed")
			}
		}
		return old, nil
	}
	if method != "client.draft.put" {
		return nil, fmt.Errorf("unknown draft operation")
	}
	var next Draft
	if e = decode(p["draft"], &next); e != nil {
		return nil, e
	}
	if len(next.Files) > 16 || len(next.Text) > 4<<20 {
		return nil, fmt.Errorf("draft exceeds storage limit")
	}
	known := map[string]bool{}
	for _, f := range old.Files {
		known[f.Hash] = true
	}
	total := 0
	for _, f := range next.Files {
		total += f.Size
		if f.Size < 0 || f.Size > 50<<20 || total > 100<<20 || len(f.Hash) != 64 {
			return nil, fmt.Errorf("invalid draft attachment")
		}
		if f.Data != nil || f.Size == 0 {
			if len(f.Data) != f.Size || Hash(f.Data) != f.Hash {
				return nil, fmt.Errorf("invalid draft attachment data")
			}
		} else if !known[f.Hash] {
			return nil, fmt.Errorf("missing draft attachment data")
		}
	}
	for i := range next.Files {
		f := &next.Files[i]
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if !known[f.Hash] {
			if e = store.Put(key+":"+f.Hash, f.Data); e != nil {
				return nil, e
			}
		}
		f.Data = nil
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if next.Text == "" && next.Model == "" && len(next.Files) == 0 {
		e = store.Delete(key)
	} else {
		e = store.Put(key, next)
	}
	if e != nil {
		return nil, e
	}
	// Also collect any bytes left by an interrupted previous write.
	keys, e := store.Keys()
	if e != nil {
		return nil, e
	}
	keep := map[string]bool{}
	for _, f := range next.Files {
		keep[key+":"+f.Hash] = true
	}
	for _, k := range keys {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		if strings.HasPrefix(k, key+":") && !keep[k] {
			if e = store.Delete(k); e != nil {
				return nil, e
			}
		}
	}
	return nil, nil
}
