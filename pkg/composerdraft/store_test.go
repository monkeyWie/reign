package composerdraft

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type memoryStore struct {
	values map[string]json.RawMessage
	fail   string
}

func (s *memoryStore) Get(k string) (any, error) {
	var v any
	if b := s.values[k]; b != nil {
		if e := json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
	}
	return v, nil
}
func (s *memoryStore) Put(k string, v any) error {
	if k == s.fail {
		return errors.New("disk full")
	}
	b, e := json.Marshal(v)
	if e == nil {
		s.values[k] = b
	}
	return e
}
func (s *memoryStore) Delete(k string) error { delete(s.values, k); return nil }
func (s *memoryStore) Keys() ([]string, error) {
	var out []string
	for k := range s.values {
		out = append(out, k)
	}
	return out, nil
}
func TestDraftFailedCommitRecoveryAndValidation(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{values: map[string]json.RawMessage{}}
	p := map[string]any{"provider": "codex", "session": "s", "draft": Draft{Text: "before"}}
	if _, e := Call(ctx, store, "client.draft.put", "d", p); e != nil {
		t.Fatal(e)
	}
	data := []byte("image fixture")
	file := File{Name: "image.png", Hash: Hash(data), Size: len(data), Data: data}
	p["draft"] = Draft{Text: "after", Files: []File{file}}
	store.fail = Key("d", "codex", "s")
	if _, e := Call(ctx, store, "client.draft.put", "d", p); e == nil {
		t.Fatal("commit succeeded")
	}
	v, e := Call(ctx, store, "client.draft.get", "d", p)
	if e != nil || v.(Draft).Text != "before" {
		t.Fatal("failed write damaged old draft", v, e)
	}
	store.fail = ""
	if _, e = Call(ctx, store, "client.draft.put", "d", p); e != nil {
		t.Fatal(e)
	}
	v, e = Call(ctx, store, "client.draft.get", "d", p)
	if e != nil || string(v.(Draft).Files[0].Data) != string(data) {
		t.Fatal("retry failed", e)
	}
	file.Data = nil
	file.Hash = strings.Repeat("0", 64)
	p["draft"] = Draft{Text: "bad", Files: []File{file}}
	if _, e = Call(ctx, store, "client.draft.put", "d", p); e == nil {
		t.Fatal("missing bytes accepted")
	}
	file.Data = data
	file.Size = 50<<20 + 1
	p["draft"] = Draft{Files: []File{file}}
	if _, e = Call(ctx, store, "client.draft.put", "d", p); e == nil {
		t.Fatal("oversize accepted")
	}
	p["draft"] = Draft{}
	if _, e = Call(ctx, store, "client.draft.put", "d", p); e != nil {
		t.Fatal(e)
	}
	if len(store.values) != 0 {
		t.Fatal("removed attachments leaked")
	}
}
