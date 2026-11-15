package webapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
)

type memoryStore struct {
	mu sync.Mutex
	v  map[string][]byte
}

func (s *memoryStore) Get(k string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.v[k]
	if b == nil {
		return nil, nil
	}
	var v any
	e := json.Unmarshal(b, &v)
	return v, e
}
func (s *memoryStore) Put(k string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := json.Marshal(v)
	s.v[k] = b
	return e
}
func (s *memoryStore) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.v, k)
	return nil
}
func (s *memoryStore) Keys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for k := range s.v {
		out = append(out, k)
	}
	return out, nil
}
func TestOfflineDeviceIsolationAndForget(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{v: map[string][]byte{}}
	a := New(s)
	defer a.Close()
	_ = s.Put("devices", []Object{{"id": "one", "key": "secret", "expires": 0}, {"id": "two", "key": "other", "expires": 0}})
	v, e := a.Call(ctx, "client.list", nil, "", nil)
	if e != nil || len(arr(v)) != 2 || str(arr(v)[0], "key") != "" {
		t.Fatal("exposed pairing credential", v, e)
	}
	for _, id := range []string{"one", "two"} {
		_, e = a.Call(ctx, "client.assetCache.put", Object{"key": "photo", "data": base64.StdEncoding.EncodeToString([]byte(id))}, id, nil)
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = a.Call(ctx, "client.disconnect", Object{"id": "one"}, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	v, _ = a.Call(ctx, "client.list", nil, "", nil)
	if arr(v)[0]["autoReconnect"] == true {
		t.Fatal("disconnect did not stop automatic reconnect")
	}
	_, e = a.Call(ctx, "client.forget", Object{"id": "one"}, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	v, _ = a.Call(ctx, "client.assetCache.get", Object{"key": "photo"}, "one", nil)
	if v != nil {
		t.Fatal("forgotten device still cached")
	}
	v, _ = a.Call(ctx, "client.assetCache.get", Object{"key": "photo"}, "two", nil)
	if str(obj(v), "data") == "" {
		t.Fatal("forget removed other device's cache")
	}
}

func TestCachedCatalogueAndStreamedSnapshotWithoutConnection(t *testing.T) {
	s := &memoryStore{v: map[string][]byte{}}
	a := New(s)
	defer a.Close()
	ctx := context.Background()
	for _, target := range []string{"one", "two"} {
		b, _ := json.Marshal(Object{"method": "providers", "provider": "", "session": ""})
		_ = s.Put("rpc:"+target+":"+string(b), []Object{{"id": "codex-app"}})
		_ = s.Put("directory:"+target+":codex-app", Object{"items": Object{"chat": Object{"id": target, "providerId": "codex-app"}}})
		if _, err := a.Call(ctx, "client.snapshot", Object{"provider": "codex-app", "session": "chat", "transcript": Object{"turns": []Object{{"id": "streamed"}}}}, target, nil); err != nil {
			t.Fatal(err)
		}
	}
	v, err := a.Call(ctx, "client.cached", nil, "one", nil)
	if err != nil || len(arr(obj(v)["sessions"])) != 1 || str(arr(obj(v)["sessions"])[0], "id") != "one" {
		t.Fatal("cached directory mixed devices", v, err)
	}
	r := Object{"method": "transcript", "provider": "codex-app", "session": "chat"}
	v, err = a.Call(ctx, "client.cached", nil, "one", r)
	if err != nil || str(arr(obj(v)["turns"])[0], "id") != "streamed" {
		t.Fatal("lost cached deltas", v, err)
	}
	if _, err = a.Call(ctx, "client.cached", nil, "one", Object{"method": "send"}); err == nil {
		t.Fatal("cache accepted a mutation")
	}
	if _, err = a.Call(ctx, "client.forget", Object{"id": "one"}, "", nil); err != nil {
		t.Fatal(err)
	}
	if v, err = a.Call(ctx, "client.cached", nil, "one", r); v != nil || err != nil {
		t.Fatal("forgotten snapshot retained", v, err)
	}
	if v, err = a.Call(ctx, "client.cached", nil, "two", r); v == nil || err != nil {
		t.Fatal("other device's snapshot removed", v, err)
	}
}
