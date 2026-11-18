package assets

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uploadBytes(t *testing.T, s *Store, scope string, data []byte) providerUpload {
	t.Helper()
	call := func(method string, p Params) any {
		t.Helper()
		v, e := s.Handle(context.Background(), scope, "attachment."+method, p)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	id := call("begin", Params{Name: "../../bad\\report.txt", Size: int64(len(data)), SHA256: Hash(data)}).(UploadState).ID
	for offset := 0; offset < len(data); {
		end := min(offset+ChunkSize, len(data))
		p := Params{ID: id, Offset: int64(offset), Data: base64.StdEncoding.EncodeToString(data[offset:end])}
		call("write", p)
		call("write", p) // Lost replies are safe to replay.
		s = New(s.Root)
		state, err := s.Handle(context.Background(), scope, "attachment.status", Params{ID: id})
		if err != nil || state.(UploadState).Offset != int64(end) {
			t.Fatal(state, err)
		}
		offset = end
	}
	finished := call("finish", Params{ID: id}).(UploadState)
	again := call("status", Params{ID: id}).(UploadState)
	if again.Attachment == nil || again.Offset != int64(len(data)) {
		t.Fatal("finished status must survive lost finish reply")
	}
	if finished.Attachment.Name != "report.txt" {
		t.Fatal(finished.Attachment.Name)
	}
	return providerUpload{id: id}
}

type providerUpload struct{ id string }

func readBytes(t *testing.T, s *Store, scope, id string, thumb bool) []byte {
	t.Helper()
	var data []byte
	for {
		v, e := s.Handle(context.Background(), scope, "attachment.read", Params{ID: id, Offset: int64(len(data)), Thumbnail: thumb})
		if e != nil {
			t.Fatal(e)
		}
		c := v.(Chunk)
		b, e := base64.StdEncoding.DecodeString(c.Data)
		if e != nil || len(b) > ChunkSize {
			t.Fatal("invalid block", e)
		}
		data = append(data, b...)
		if c.EOF {
			if int64(len(data)) != c.Size || Hash(data) != c.SHA256 {
				t.Fatal("bad checksum")
			}
			return data
		}
	}
}
func TestUploadResumeScopeAndImmutableInput(t *testing.T) {
	s := New(t.TempDir())
	scope := Scope("codex-app", "one")
	data := bytes.Repeat([]byte("test file bytes"), 10000)
	u := uploadBytes(t, s, scope, data)
	if !bytes.Equal(readBytes(t, s, scope, u.id, false), data) {
		t.Fatal("data changed")
	}
	for _, method := range []string{"read", "finish", "status", "write", "abort"} {
		if _, e := s.Handle(context.Background(), Scope("codex-cli", "one"), "attachment."+method, Params{ID: u.id}); e == nil {
			t.Fatal("cross-provider access", method)
		}
	}
	_, path, e := s.Input(scope, u.id)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, []byte("agent edited input"), 0600); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(readBytes(t, s, scope, u.id, false), data) {
		t.Fatal("input mutated archive")
	}
}
func TestImportPreservesTempImageAcrossRestarts(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(t.TempDir(), "clipboard.png")
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	img.Set(1, 1, color.White)
	var raw bytes.Buffer
	if e := png.Encode(&raw, img); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(path, raw.Bytes(), 0600)
	s := New(root)
	scope := Scope("codex-app", "session")
	a := s.Import(context.Background(), scope, "turn/item", path, false)
	if a.State != "ready" || !a.Thumbnail || a.Width != 32 || a.Height != 16 || a.Kind != "image" {
		t.Fatalf("bad image metadata: %+v", a)
	}
	os.Remove(path)
	s = New(root)
	again := s.Import(context.Background(), scope, "turn/item", path, false)
	if again.ID != a.ID || again.State != "ready" || !bytes.Equal(readBytes(t, s, scope, a.ID, false), raw.Bytes()) {
		t.Fatal("temporary image not preserved")
	}
	if len(readBytes(t, s, scope, a.ID, true)) == 0 {
		t.Fatal("thumbnail empty")
	}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw.Bytes())
	for range 2 {
		a = s.Import(context.Background(), scope, "inline", dataURI, false)
		if a.Source != "" || a.Kind != "image" || a.SHA256 != Hash(raw.Bytes()) {
			t.Fatal("inline image invalid")
		}
	}
}
func TestUploadRejectsInvalidDataAndAborts(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	scope := Scope("p", "s")
	for _, p := range []Params{{Size: MaxFileSize + 1, SHA256: Hash(nil)}, {Size: -1, SHA256: Hash(nil)}, {Size: 1, SHA256: "invalid"}} {
		if _, e := s.Handle(ctx, scope, "attachment.begin", p); e == nil {
			t.Fatal("invalid begin accepted")
		}
	}
	v, e := s.Handle(ctx, scope, "attachment.begin", Params{Name: "f", Size: 3, SHA256: Hash([]byte("abc"))})
	if e != nil {
		t.Fatal(e)
	}
	id := v.(UploadState).ID
	if _, e = s.Handle(ctx, scope, "attachment.write", Params{ID: id, Offset: 1, Data: "YQ=="}); e == nil {
		t.Fatal("out of order accepted")
	}
	s.Handle(ctx, scope, "attachment.write", Params{ID: id, Data: "eHl6"})
	if _, e = s.Handle(ctx, scope, "attachment.finish", Params{ID: id}); e == nil {
		t.Fatal("bad hash accepted")
	}
	if _, e = s.Handle(ctx, scope, "attachment.write", Params{ID: id, Data: "YWJj"}); e == nil {
		t.Fatal("changed duplicate accepted")
	}
	s.Handle(ctx, scope, "attachment.abort", Params{ID: id})
	if _, e = s.Handle(ctx, scope, "attachment.status", Params{ID: id}); e == nil {
		t.Fatal("aborted upload remains")
	}
	if _, _, e = s.Input(scope, id); e == nil {
		t.Fatal("aborted file usable by agent")
	}
}
func TestControllerCacheIsolationAndEviction(t *testing.T) {
	c := &Cache{Root: t.TempDir(), Limit: 10}
	for _, device := range []string{"one", "two"} {
		if e := c.Put(device, "same", []byte(device)); e != nil {
			t.Fatal(e)
		}
	}
	c = &Cache{Root: c.Root, Limit: 10}
	b, e := c.Get("one", "same")
	if e != nil || string(b) != "one" {
		t.Fatal("cache restart", e)
	}
	c.Clear("one")
	b, _ = c.Get("two", "same")
	if string(b) != "two" {
		t.Fatal("device isolation")
	}
	c.Put("two", "large", []byte("0123456789"))
	b, _ = c.Get("two", "same")
	if b != nil {
		t.Fatal("LRU budget not enforced")
	}
}

func TestClientUploadIdentitySurvivesLostAcknowledgements(t *testing.T) {
	s := New(t.TempDir())
	ctx := context.Background()
	data := []byte("an attachment uploaded over an unreliable connection")
	id := strings.Repeat("c", 64)
	scope := Scope("agent", "chat")
	p := Params{ID: id, Name: "photo.txt", Size: int64(len(data)), SHA256: Hash(data)}
	begin := func() UploadState {
		t.Helper()
		v, err := s.Handle(ctx, scope, "attachment.begin", p)
		if err != nil {
			t.Fatal(err)
		}
		return v.(UploadState)
	}
	if begin().ID != id || begin().Offset != 0 {
		t.Fatal("begin lost identity")
	}
	chunk := Params{ID: id, Data: base64.StdEncoding.EncodeToString(data)}
	for range 2 {
		if _, err := s.Handle(ctx, scope, "attachment.write", chunk); err != nil {
			t.Fatal("lost ACK retry failed", err)
		}
	}
	if begin().Offset != int64(len(data)) {
		t.Fatal("begin retry reset uploaded bytes")
	}
	for range 2 {
		if _, err := s.Handle(ctx, scope, "attachment.finish", Params{ID: id}); err != nil {
			t.Fatal("finish retry failed", err)
		}
	}
	if begin().Attachment == nil {
		t.Fatal("lost finish ACK did not reconcile")
	}
	if _, err := s.Handle(ctx, Scope("agent", "other-chat"), "attachment.begin", p); err == nil {
		t.Fatal("ID reuse crossed scopes")
	}
	p.Name = "different.txt"
	if _, err := s.Handle(ctx, scope, "attachment.begin", p); err == nil {
		t.Fatal("same ID accepted different metadata")
	}
}
