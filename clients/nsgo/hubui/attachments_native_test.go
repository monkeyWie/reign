//go:build !js

package hubui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/assets"
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
	var refresh func()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s := newState(cx, f)
		refresh = func() { cx.Post(s.changed) }
		s.target = "fixture"
		s.selected = Object{"id": "session", "providerId": "codex-app", "title": "附件测试"}
		s.devices = []Object{{"id": "fixture", "connected": true}}
		s.setTranscript(Object{"turns": []Object{}})
		return core.View{Style: fill, Children: []core.Node{button(cx, "fixture-paste", "粘贴测试图片", nil, "outline", func() {
			s.paste(cx, core.PasteEvent{Files: []core.PastedFile{{Name: "fixture.png", MIME: "image/png", Data: photo}}})
		}), button(cx, "fixture-refresh", "刷新状态", nil, "outline", s.changed), button(cx, "fixture-publish", "显示已上传图片", nil, "outline", func() {
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
	uploadImage := app.Find("role:image fixture.png").ID
	app.WaitUntil(3*time.Second, func() bool { return f.finished.Load() && !app.Exists("id:upload-progress-fixture.png") })
	for range 3 {
		app.Press("id:fixture-refresh")
		if app.Find("role:image fixture.png").ID != uploadImage {
			t.Fatal("progress or polling remounted the upload image")
		}
	}
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
	previewImage := app.Find("id:preview-image").ID
	refresh()
	app.Settle()
	if app.Find("id:preview-image").ID != previewImage {
		t.Fatal("status update remounted the full-size image")
	}
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

// Drop successful responses rather than requests: retry must reconcile the
// receiver's already-written bytes and finished object without a second upload.
type unreliableUploadAPI struct {
	*attachmentFixture
	writes                           chan struct{}
	finish                           chan struct{}
	lostBegin, lostWrite, lostFinish atomic.Bool
	starts, completed                atomic.Int32
}

func (f *unreliableUploadAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	m := str(r, "method")
	if method == "client.call" && (m == "attachment.write" || m == "attachment.finish") {
		gate := f.writes
		if m == "attachment.finish" {
			gate = f.finish
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-gate:
		}
	}
	v, err := f.attachmentFixture.Call(ctx, method, p, target, r)
	if method != "client.call" || err != nil {
		return v, err
	}
	lost := false
	switch m {
	case "attachment.begin":
		f.starts.Add(1)
		lost = f.lostBegin.CompareAndSwap(false, true)
	case "attachment.write":
		lost = f.lostWrite.CompareAndSwap(false, true)
	case "attachment.finish":
		f.completed.Add(1)
		lost = f.lostFinish.CompareAndSwap(false, true)
	}
	if lost {
		return nil, fmt.Errorf("fixture: acknowledgement lost")
	}
	return v, nil
}

func waitForReadyUploads(app *nsgotest.App, s *State, count int) {
	var ready atomic.Bool
	app.WaitUntil(4*time.Second, func() bool {
		// Query only reports visible views. A queued card below the viewport
		// has no visible progress either, so absence cannot prove completion.
		app.Post(func() {
			s.cx.Post(func() {
				complete := len(s.uploads) == count && s.uploadActive == 0
				for _, u := range s.uploads {
					complete = complete && u.Status == "ready"
				}
				ready.Store(complete)
			})
		})
		return ready.Load()
	})
}

func TestFourUploadsQueueRetryLostACKsAndAwaitConfirmationBelow100(t *testing.T) {
	f := &unreliableUploadAPI{attachmentFixture: &attachmentFixture{demoAPI: newDemoAPI(), store: assets.New(t.TempDir())}, writes: make(chan struct{}), finish: make(chan struct{})}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.target = "fixture"
		s.selected = Object{"id": "session", "providerId": "codex-app"}
		s.devices = []Object{{"id": "fixture", "connected": true}}
		return core.View{Style: fill, Children: []core.Node{button(cx, "four-files", "Choose files", nil, "outline", func() {
			for _, name := range []string{"one.txt", "two.txt", "three.txt", "four.txt"} {
				s.addUpload(name, "text/plain", bytes.Repeat([]byte("sample"), 15000))
			}
		}), s.uploadPreviews(cx)}}
	}, nsgotest.Size(390, 844), nsgotest.Environment(core.Environment{Locale: "en-US"}))
	app.Press("id:four-files")
	app.WaitUntil(2*time.Second, func() bool { return f.starts.Load() >= 3 })
	app.Find("id:upload-four.txt")
	app.Post(func() {
		s.cx.Post(func() {
			if s.uploadActive != 2 || s.uploads[2].Status != "queued" || s.uploads[3].Status != "queued" {
				t.Error("fourth upload did not wait safely")
			}
		})
	})
	app.Settle()
	close(f.writes)
	app.WaitUntil(3*time.Second, func() bool {
		return app.Find("id:upload-progress-one.txt").Text == "99%" && app.Find("id:upload-progress-two.txt").Text == "99%"
	})
	if app.Exists("text:已上传，等待设备校验确认") || app.Exists("text:上传 100%") {
		t.Fatal("compact upload shows prose or premature completion")
	}
	app.Screenshot("four-uploads-awaiting-confirmation")
	close(f.finish)
	waitForReadyUploads(app, s, 4)
}

func TestUploadRemovalCancelsActiveAndSkipsQueuedFile(t *testing.T) {
	f := &unreliableUploadAPI{attachmentFixture: &attachmentFixture{demoAPI: newDemoAPI(), store: assets.New(t.TempDir())}, writes: make(chan struct{}), finish: make(chan struct{})}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.target = "fixture"
		s.selected = Object{"id": "session", "providerId": "codex-app"}
		s.devices = []Object{{"id": "fixture", "connected": true}}
		return core.View{Style: fill, Children: []core.Node{button(cx, "choose-files", "Choose files", nil, "outline", func() {
			for _, name := range []string{"one.txt", "two.txt", "three.txt", "four.txt"} {
				s.addUpload(name, "text/plain", []byte("fixture"))
			}
		}), s.uploadPreviews(cx)}}
	}, nsgotest.Size(390, 844))
	app.Press("id:choose-files")
	app.WaitUntil(2*time.Second, func() bool { return f.starts.Load() >= 3 })
	app.Press("id:upload-four.txt >> role:button")
	app.Press("id:upload-one.txt >> role:button")
	app.WaitUntil(time.Second, func() bool { return f.starts.Load() >= 4 })
	if app.Exists("id:upload-one.txt") || app.Exists("id:upload-four.txt") {
		t.Fatal("removed upload remains visible")
	}
	close(f.writes)
	close(f.finish)
	waitForReadyUploads(app, s, 2)
	if f.starts.Load() != 4 {
		t.Fatal("cancelled queued upload reached host", f.starts.Load())
	}
}
