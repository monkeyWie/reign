package webapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/composerdraft"
)

func TestControllerDraftPersistence(t *testing.T) {
	testDraftStorage(t, &memoryStore{v: map[string][]byte{}})
}
func testDraftStorage(t *testing.T, s Storage) {
	t.Helper()
	ctx := context.Background()
	target := fmt.Sprintf("draft-fixture-%d", time.Now().UnixNano())
	p := Object{"provider": "codex", "session": "session"}
	defer func() {
		_, err := New(s).Call(ctx, "client.draft.put", Object{"provider": "codex", "session": "session", "draft": composerdraft.Draft{}}, target, nil)
		if err != nil {
			t.Error(err)
		}
	}()
	data := []byte{0, 1, 2, 255, 128}
	p["draft"] = composerdraft.Draft{Text: "offline draft 🙂", Files: []composerdraft.File{{Name: "fixture.bin", MIME: "application/octet-stream", Hash: composerdraft.Hash(data), Size: len(data), Data: data}}}
	a := New(s)
	if _, err := a.Call(ctx, "client.draft.put", p, target, nil); err != nil {
		t.Fatal(err)
	}
	a.Close()
	a = New(s)
	defer a.Close()
	if _, err := a.Call(ctx, "client.assetCache.clear", nil, target, nil); err != nil {
		t.Fatal(err)
	}
	v, err := a.Call(ctx, "client.draft.get", p, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := v.(composerdraft.Draft)
	if d.Text != "offline draft 🙂" || len(d.Files) != 1 || string(d.Files[0].Data) != string(data) {
		t.Fatal("draft did not survive controller restart/cache clearing")
	}
}
