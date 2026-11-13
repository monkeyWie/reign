//go:build js && wasm

package webapi

import (
	"encoding/json"
	"errors"
	"sync"
	"syscall/js"
)

// Store keeps connection metadata, transcript snapshots and bounded attachment
// caches in IndexedDB. No attachment bytes or credentials go into localStorage.
type Store struct {
	mu sync.Mutex
	db js.Value
}

func request(r js.Value) (js.Value, error) {
	type result struct {
		v   js.Value
		err error
	}
	ch := make(chan result, 1)
	ok := js.FuncOf(func(js.Value, []js.Value) any { ch <- result{v: r.Get("result")}; return nil })
	bad := js.FuncOf(func(js.Value, []js.Value) any {
		ch <- result{err: errors.New("IndexedDB: " + r.Get("error").Get("message").String())}
		return nil
	})
	r.Set("onsuccess", ok)
	r.Set("onerror", bad)
	v := <-ch
	r.Set("onsuccess", js.Null())
	r.Set("onerror", js.Null())
	ok.Release()
	bad.Release()
	return v.v, v.err
}
func (s *Store) open() error {
	if s.db.Truthy() {
		return nil
	}
	factory := js.Global().Get("indexedDB")
	if !factory.Truthy() {
		return errors.New("此浏览器不支持 IndexedDB")
	}
	r := factory.Call("open", "agenthub-nsgo-v1", 1)
	upgrade := js.FuncOf(func(js.Value, []js.Value) any {
		db := r.Get("result")
		db.Call("createObjectStore", "cache")
		return nil
	})
	r.Set("onupgradeneeded", upgrade)
	db, e := request(r)
	r.Set("onupgradeneeded", js.Null())
	upgrade.Release()
	if e == nil {
		s.db = db
	}
	return e
}
func (s *Store) Get(key string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.open(); e != nil {
		return nil, e
	}
	r, e := request(s.db.Call("transaction", "cache", "readonly").Call("objectStore", "cache").Call("get", key))
	if e != nil || !r.Truthy() {
		return nil, e
	}
	var v any
	e = json.Unmarshal([]byte(r.String()), &v)
	return v, e
}
func (s *Store) Put(key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.write("put", string(b), key)
}
func (s *Store) Delete(key string) error { return s.write("delete", key) }
func (s *Store) write(method string, args ...any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.open(); e != nil {
		return e
	}
	tx := s.db.Call("transaction", "cache", "readwrite")
	ch := make(chan error, 1)
	ok := js.FuncOf(func(js.Value, []js.Value) any { ch <- nil; return nil })
	bad := js.FuncOf(func(js.Value, []js.Value) any {
		ch <- errors.New("浏览器缓存写入失败：存储空间不足或事务中断")
		return nil
	})
	tx.Set("oncomplete", ok)
	tx.Set("onabort", bad)
	tx.Call("objectStore", "cache").Call(method, args...)
	e := <-ch
	tx.Set("oncomplete", js.Null())
	tx.Set("onabort", js.Null())
	ok.Release()
	bad.Release()
	return e
}
func (s *Store) Keys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.open(); e != nil {
		return nil, e
	}
	r, e := request(s.db.Call("transaction", "cache", "readonly").Call("objectStore", "cache").Call("getAllKeys"))
	if e != nil {
		return nil, e
	}
	out := []string{}
	for i := 0; i < r.Length(); i++ {
		out = append(out, r.Index(i).String())
	}
	return out, nil
}
