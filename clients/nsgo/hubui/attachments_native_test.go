//go:build !js

package hubui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/monkeyWie/reign/internal/assets"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type attachmentFixture struct {
	*demoAPI
	store    *assets.Store
	finished atomic.Bool
}

func (f *attachmentFixture) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if method == "client.call" && strings.HasPrefix(str(r, "method"), "attachment.") {
		b, _ := json.Marshal(r["params"])
		var params assets.Params
		_ = json.Unmarshal(b, &params)
		v, e := f.store.Handle(ctx, assets.Scope(str(r, "provider"), str(r, "session")), str(r, "method"), params)
		if e != nil {
			return nil, e
		}
		if str(r, "method") == "attachment.finish" {
			f.finished.Store(true)
		}
		b, _ = json.Marshal(v)
		var out any
		_ = json.Unmarshal(b, &out)
		return out, nil
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}
func TestClipboardUploadAndImageGallery(t *testing.T) {
	f := &attachmentFixture{demoAPI: newDemoAPI(), store: assets.New(t.TempDir())}
	img := image.NewRGBA(image.Rect(0, 0, 128, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 128; x++ {
			img.Set(x, y, color.RGBA{R: 80, G: 120, B: 200, A: 255})
		}
	}
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, img)
	photo := buffer.Bytes()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s := newState(cx, f)
		s.target = "fixture"
		s.selected = Object{"id": "session", "providerId": "codex-app", "title": "附件测试"}
		s.devices = []Object{{"id": "fixture", "connected": true}}
		s.setTranscript(Object{"turns": []Object{}})
		return core.View{Style: fill, Children: []core.Node{button(cx, "fixture-paste", "粘贴测试图片", nil, "outline", func() {
			s.paste(cx, core.PasteEvent{Files: []core.PastedFile{{Name: "fixture.png", MIME: "image/png", Data: photo}}})
		}), button(cx, "fixture-publish", "显示已上传图片", nil, "outline", func() {
			if len(s.uploads) == 0 || s.uploads[0].Status != "ready" {
				return
			}
			a, _, e := f.store.Input(assets.Scope("codex-app", "session"), s.uploads[0].ID)
			if e != nil {
				panic(e)
			}
			b, _ := json.Marshal(a)
			meta := Object{}
			_ = json.Unmarshal(b, &meta)
			other := copyObject(meta)
			other["id"] = "second"
			s.images[s.key()+"|second|"+"true"] = photo
			s.images[s.key()+"|second|false"] = photo
			s.setTranscript(Object{"turns": []Object{{"id": "t", "status": "completed", "items": []Object{{"id": "u", "role": "user", "text": "两张图片", "attachments": []Object{meta, other}}}}}})
		}), s.conversation(cx, false), core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) })}}
	}, nsgotest.Size(1000, 800))
	if nsgotest.Target() == "" {
		app.Paste("id:chat-input", core.PasteEvent{Files: []core.PastedFile{{Name: "fixture.png", MIME: "image/png", Data: photo}}})
	} else {
		app.Press("id:fixture-paste")
	}
	app.WaitFor("id:upload-fixture.png", 3*time.Second)
	app.WaitUntil(3*time.Second, func() bool { return f.finished.Load() && !app.Exists("id:upload-progress-fixture.png") })
	app.Press("id:fixture-publish")
	app.WaitFor("id:image-second", 3*time.Second)
	app.Press("id:image-second")
	app.Find("id:image-preview")
	app.Find("text:2 / 2")
	app.Press("id:preview-previous")
	app.Find("text:1 / 2")
	app.WaitUntil(3*time.Second, func() bool {
		v := app.Find("id:preview-image")
		return len(v.Image.Source.Data) > 0 && v.Frame.Size.Width > 100 && v.Frame.Size.Height > 100
	})
	app.Screenshot("attachment-gallery")
	app.Press("id:preview-close")
	// Independently read back the finished bytes through the same scoped RPCs.
	scope := assets.Scope("codex-app", "session")
	begin, e := f.store.Handle(context.Background(), scope, "attachment.begin", assets.Params{Name: "readback.txt", Size: 4, SHA256: hashBytes([]byte("file"))})
	if e != nil {
		t.Fatal(e)
	}
	u := begin.(assets.UploadState)
	_, _ = f.store.Handle(context.Background(), scope, "attachment.write", assets.Params{ID: u.ID, Data: base64.StdEncoding.EncodeToString([]byte("file"))})
	end, e := f.store.Handle(context.Background(), scope, "attachment.finish", assets.Params{ID: u.ID})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(end.(assets.UploadState).Attachment)
	a := Object{}
	_ = json.Unmarshal(b, &a)
	s := &State{api: f}
	data, e := s.attachmentBytes(context.Background(), "fixture", "codex-app", "session", a, false)
	if e != nil || string(data) != "file" {
		t.Fatal("attachment readback", string(data), e)
	}
}
