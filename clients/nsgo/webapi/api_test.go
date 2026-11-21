package webapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"sync"
	"testing"
)

type memoryStore struct {
	histories map[persistence.Session]Object
	dirs      map[persistence.DirectoryKey]map[string]Object
	heads     map[persistence.DirectoryKey]persistence.DirectoryHead
	mu        sync.Mutex
	v         map[string][]byte
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
	for _, d := range []Object{{"id": "one", "key": "secret", "expires": 0}, {"id": "two", "key": "other", "expires": 0}} {
		_ = s.SaveEnvironment(context.Background(), d)
	}
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
		_ = s.WriteDirectory(ctx, persistence.DirectoryKey{Env: target, Provider: "codex-app"}, persistence.DirectoryHead{}, persistence.DirectoryBatch{Upserts: []Object{{"id": target, "providerId": "codex-app"}}})
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

func TestFirstPageMetadataSurvivesBeforeDirectorySync(t *testing.T) {
	s := &memoryStore{v: map[string][]byte{}}
	a := New(s)
	defer a.Close()
	put := func(command Object, v any) {
		t.Helper()
		raw, _ := json.Marshal(command)
		if err := s.Put("rpc:one:"+string(raw), v); err != nil {
			t.Fatal(err)
		}
	}
	put(Object{"method": "providers", "provider": "", "session": ""}, []Object{{"id": "codex-app"}})
	_ = s.WriteDirectory(context.Background(), persistence.DirectoryKey{Env: "one", Provider: "codex-app"}, persistence.DirectoryHead{}, persistence.DirectoryBatch{Preview: true, Upserts: []Object{{"id": "preview", "providerId": "codex-app"}}})
	v, err := a.Call(context.Background(), "client.cached", nil, "one", nil)
	if err != nil || len(arr(obj(v)["sessions"])) != 1 {
		t.Fatal("metadata page not cached", v, err)
	}
	if err = s.WriteDirectory(context.Background(), persistence.DirectoryKey{Env: "one", Provider: "codex-app"}, persistence.DirectoryHead{}, persistence.DirectoryBatch{Reset: true}); err != nil {
		t.Fatal(err)
	}
	v, err = a.Call(context.Background(), "client.cached", nil, "one", nil)
	if err != nil || len(arr(obj(v)["sessions"])) != 0 {
		t.Fatal("preview resurrected deleted session", v, err)
	}
}

func TestFreshReadCannotConfirmOfflineCache(t *testing.T) {
	store := &memoryStore{v: map[string][]byte{}}
	a := New(store)
	defer a.Close()
	command := Object{"method": "transcript", "provider": "codex-app", "session": "chat"}
	_ = store.ApplyHistory(context.Background(), persistence.Session{Env: "one", Provider: "codex-app", ID: "chat"}, persistence.Diff("", nil, Object{"turns": []Object{}}))
	if v, e := a.Call(context.Background(), "client.call", nil, "one", command); e != nil || v == nil {
		t.Fatal(v, e)
	}
	command["fresh"] = true
	if _, e := a.Call(context.Background(), "client.call", nil, "one", command); e == nil {
		t.Fatal("offline cache accepted as fresh confirmation")
	}
}

func TestOfflineSyncRetainsMetadataBeforeCompleteDirectory(t *testing.T) {
	ctx := context.Background()
	disk := &memoryStore{v: map[string][]byte{}}
	a := New(disk)
	_ = disk.WriteDirectory(ctx, persistence.DirectoryKey{Env: "one", Provider: "codex-app"}, persistence.DirectoryHead{}, persistence.DirectoryBatch{Preview: true, Upserts: []Object{{"id": "preview", "providerId": "codex-app"}}})
	if _, err := a.Call(ctx, "client.sync", Object{"provider": "codex-app"}, "one", nil); err == nil {
		t.Fatal("offline sync claimed successful deletion")
	}
	a.Close()
	a = New(disk)
	defer a.Close()
	v, err := a.Call(ctx, "client.cached", nil, "one", nil)
	if err != nil || len(arr(obj(v)["sessions"])) != 1 {
		t.Fatal("lost sessions after reopening", v, err)
	}
}
