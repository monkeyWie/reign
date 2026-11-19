//go:build js && wasm

package webapi

import (
	"fmt"
	"testing"
	"time"
)

func browserStorage(t *testing.T) {
	testDraftStorage(t, &Store{})
	key := fmt.Sprintf("fixture-%d", time.Now().UnixNano())
	s := &Store{}
	if e := s.Put(key, Object{"text": "离线缓存🙂", "cursor": 4}); e != nil {
		t.Fatal(e)
	}
	s2 := &Store{}
	v, e := s2.Get(key)
	if e != nil || str(obj(v), "text") != "离线缓存🙂" || num(obj(v), "cursor") != 4 {
		t.Fatal(v, e)
	}
	if e = s2.Delete(key); e != nil {
		t.Fatal(e)
	}
	v, e = s.Get(key)
	if e != nil || v != nil {
		t.Fatal("delete was not durable", v, e)
	}
}
