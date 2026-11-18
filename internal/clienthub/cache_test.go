package clienthub

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monkeyWie/reignhub/internal/control"
)

func TestLocalSnapshotsAndDirectoriesAreIndependentOfConnection(t *testing.T) {
	h, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	for _, id := range []string{"one", "two"} {
		r := control.Request{Method: "providers"}
		b, _ := json.Marshal(r)
		if err = h.DB.Put("rpc:"+id+":"+string(b), []map[string]any{{"id": "codex-app"}}); err != nil {
			t.Fatal(err)
		}
		if err = h.DB.Put("directory:"+id+":codex-app", map[string]any{"items": map[string]any{"chat": map[string]any{"id": id, "providerId": "codex-app"}}}); err != nil {
			t.Fatal(err)
		}
		if err = h.SaveSnapshot(id, "codex-app", "chat", json.RawMessage(`{"turns":[{"id":"latest-delta"}]}`)); err != nil {
			t.Fatal(err)
		}
	}
	v, err := h.Cached("one", control.Request{})
	if err != nil {
		t.Fatal(err)
	}
	catalogue := v.(map[string]any)
	if sessions := catalogue["sessions"].([]map[string]any); len(sessions) != 1 || sessions[0]["id"] != "one" {
		t.Fatal("cache mixed devices", catalogue)
	}
	v, err = h.Cached("one", control.Request{Method: "transcript", Provider: "codex-app", Session: "chat"})
	if err != nil || v == nil {
		t.Fatal("offline projection missing", v, err)
	}
	if _, err = h.Cached("one", control.Request{Method: "send"}); err == nil {
		t.Fatal("mutation accepted as a cache read")
	}
	if v, err = h.Cached("missing", control.Request{Method: "transcript"}); v != nil || err != nil {
		t.Fatal("cache miss must be immediate and empty", v, err)
	}
	if _, err = h.Call(context.Background(), "one", control.Request{Method: "send"}); err == nil {
		t.Fatal("offline cache enabled a write")
	}
	if err = h.Forget("one"); err != nil {
		t.Fatal(err)
	}
	if v, err = h.Cached("one", control.Request{Method: "transcript", Provider: "codex-app", Session: "chat"}); v != nil || err != nil {
		t.Fatal("forgotten transcript retained", v, err)
	}
	if v, err = h.Cached("two", control.Request{Method: "transcript", Provider: "codex-app", Session: "chat"}); v == nil || err != nil {
		t.Fatal("forget removed other device", v, err)
	}
}

func TestFirstPageIsCachedBeforeDirectoryScanCompletes(t *testing.T) {
	h, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	put := func(r control.Request, v any) {
		t.Helper()
		raw, _ := json.Marshal(r)
		if err := h.DB.Put("rpc:one:"+string(raw), v); err != nil {
			t.Fatal(err)
		}
	}
	put(control.Request{Method: "providers"}, []map[string]any{{"id": "codex-app"}})
	put(control.Request{Method: "sessions", Provider: "codex-app"}, map[string]any{"sessions": []map[string]any{{"id": "preview", "providerId": "codex-app"}}})
	v, err := h.Cached("one", control.Request{})
	if err != nil || len(v.(map[string]any)["sessions"].([]map[string]any)) != 1 {
		t.Fatal("metadata page not cached", v, err)
	}
	// Once a complete empty directory arrives, stale preview entries stay deleted.
	if err = h.DB.Put("directory:one:codex-app", map[string]any{"items": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	v, err = h.Cached("one", control.Request{})
	if err != nil || len(v.(map[string]any)["sessions"].([]map[string]any)) != 0 {
		t.Fatal("preview resurrected deleted session", v, err)
	}
}

func TestFreshReadCannotMistakeOfflineCacheForRemoteConfirmation(t *testing.T) {
	h, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	r := control.Request{Method: "transcript", Provider: "codex-app", Session: "chat"}
	b, _ := json.Marshal(r)
	if err = h.DB.Put("rpc:one:"+string(b), map[string]any{"turns": []any{}}); err != nil {
		t.Fatal(err)
	}
	if v, e := h.Call(context.Background(), "one", r); e != nil || v == nil {
		t.Fatal("ordinary cache fallback failed", v, e)
	}
	r.Fresh = true
	if _, e := h.Call(context.Background(), "one", r); e == nil {
		t.Fatal("fresh read silently returned an offline snapshot")
	}
}

func TestOfflineSyncCannotErasePersistedPreview(t *testing.T) {
	root := t.TempDir()
	h, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	r := control.Request{Method: "sessions", Provider: "codex-app"}
	key, _ := json.Marshal(r)
	if err = h.DB.Put("rpc:one:"+string(key), map[string]any{"sessions": []map[string]any{{"id": "preview", "providerId": "codex-app"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.Sync(context.Background(), "one", "codex-app"); err == nil {
		t.Fatal("offline sync claimed authoritative empty directory")
	}
	h.Close()
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	v, err := reopened.Cached("one", control.Request{})
	if err != nil || len(v.(map[string]any)["sessions"].([]map[string]any)) != 1 {
		t.Fatal("offline sync erased persisted sessions", v, err)
	}
}
