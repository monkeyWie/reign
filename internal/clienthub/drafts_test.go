package clienthub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/monkeyWie/reignhub/pkg/composerdraft"
)

func TestDraftRestartAndAttachmentOwnership(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "controller")
	h, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("durable attachment bytes")
	d := composerdraft.Draft{Text: "unsent text", Files: []composerdraft.File{{Name: "note.txt", MIME: "text/plain", Hash: composerdraft.Hash(data), Size: len(data), Data: data}}}
	p := map[string]any{"provider": "codex", "session": "same", "draft": d}
	if _, err = h.Draft(context.Background(), "client.draft.put", "local", p); err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err = h.Assets.Clear("local"); err != nil {
		t.Fatal(err)
	}
	v, err := h.Draft(context.Background(), "client.draft.get", "local", p)
	if err != nil {
		t.Fatal(err)
	}
	got := v.(composerdraft.Draft)
	if got.Text != d.Text || string(got.Files[0].Data) != string(data) {
		t.Fatal("draft or bytes lost on restart")
	}
	got.Text = "edited after restart"
	got.Files[0].Data = nil
	p["draft"] = got
	if _, err = h.Draft(context.Background(), "client.draft.put", "local", p); err != nil {
		t.Fatal(err)
	}
	v, err = h.Draft(context.Background(), "client.draft.get", "remote", p)
	if err != nil {
		t.Fatal(err)
	}
	if v.(composerdraft.Draft).Text != "" {
		t.Fatal("draft crossed device boundary")
	}
	p["draft"] = composerdraft.Draft{}
	if _, err = h.Draft(context.Background(), "client.draft.put", "local", p); err != nil {
		t.Fatal(err)
	}
	keys, err := h.DB.Keys(composerdraft.Prefix("local"))
	if err != nil || len(keys) != 0 {
		t.Fatal("consumed draft bytes remain", keys, err)
	}
}

func TestDraftCancelledWritePreservesPrevious(t *testing.T) {
	h, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	p := map[string]any{"provider": "codex", "session": "session", "draft": composerdraft.Draft{Text: "original"}}
	if _, err = h.Draft(context.Background(), "client.draft.put", "remote", p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p["draft"] = composerdraft.Draft{Text: "lost"}
	if _, err = h.Draft(ctx, "client.draft.put", "remote", p); err == nil {
		t.Fatal("cancelled write accepted")
	}
	v, err := h.Draft(context.Background(), "client.draft.get", "remote", p)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	var got composerdraft.Draft
	_ = json.Unmarshal(b, &got)
	if got.Text != "original" {
		t.Fatal("cancelled write replaced saved text")
	}
	if err = h.Forget("remote"); err != nil {
		t.Fatal(err)
	}
	keys, _ := h.DB.Keys(composerdraft.Prefix("remote"))
	if len(keys) != 0 {
		t.Fatal("forgotten drafts remain")
	}
}
