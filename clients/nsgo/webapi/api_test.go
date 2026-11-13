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
