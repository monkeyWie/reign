package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/monkeyWie/reign/internal/control"
)

func TestDurableCommandDeduplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	db, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	req := control.Request{ID: "command", Method: "send", Session: "original", Text: "hello"}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db.Execute(req, func() control.Response {
				calls.Add(1)
				return control.Response{ID: req.ID, Result: json.RawMessage(`{"ok":true}`)}
			})
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("side effect executed %d times", calls.Load())
	}
	db.Close()
	db, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	out := db.Execute(req, func() control.Response { t.Error("command replayed after restart"); return control.Response{} })
	if out.Error != "" || string(out.Result) != `{"ok":true}` {
		t.Fatalf("bad saved response %+v", out)
	}
	req.Text = "different"
	if out = db.Execute(req, func() control.Response { t.Error("ID collision executed"); return control.Response{} }); out.Error == "" {
		t.Fatal("conflicting command accepted")
	}
	unknown := control.Request{ID: "crashed", Method: "send"}
	b, _ := json.Marshal(unknown)
	if _, e = db.Exec("INSERT INTO commands(id,request,state) VALUES(?,?,'pending')", unknown.ID, b); e != nil {
		t.Fatal(e)
	}
	out = db.Execute(unknown, func() control.Response { t.Error("crashed command replayed"); return control.Response{} })
	if !strings.Contains(out.Error, "outcomeUnknown") {
		t.Fatal(out)
	}
}
func TestDirectoryDeltaDeletionAndCursorReset(t *testing.T) {
	db, e := Open(filepath.Join(t.TempDir(), "cache.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	first := map[string]json.RawMessage{"one": json.RawMessage(`{"id":"one","title":"old"}`), "two": json.RawMessage(`{"id":"two"}`)}
	if e = db.Replace("provider", first); e != nil {
		t.Fatal(e)
	}
	initial, e := db.Delta("provider", "epoch", 0)
	if e != nil || !initial.Reset || len(initial.Items) != 2 {
		t.Fatalf("snapshot %+v %v", initial, e)
	}
	if e = db.Replace("provider", map[string]json.RawMessage{"one": json.RawMessage(`{"id":"one","title":"new"}`)}); e != nil {
		t.Fatal(e)
	}
	next, e := db.Delta("provider", "epoch", initial.Cursor)
	if e != nil || next.Reset || len(next.Changes) != 2 {
		t.Fatalf("delta %+v %v", next, e)
	}
	deletes := 0
	for _, change := range next.Changes {
		if change.Deleted && change.ID == "two" {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatal("missing tombstone")
	}
	reset, e := db.Delta("provider", "new-epoch", 999999)
	if e != nil || !reset.Reset || len(reset.Items) != 1 {
		t.Fatalf("lost DB cursor didn't reset %+v %v", reset, e)
	}
}
