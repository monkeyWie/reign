package hubui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestCodeBlockCopyUsesIconAndFeedback(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, newDemoAPI())
		s.applyColors()
		return core.View{Style: fill, Children: []core.Node{s.message(cx, func() Message {
			return Message{ID: "copy", Role: "assistant", Text: "```go\nfmt.Println(\"hello\")\n```"}
		})}}
	}, nsgotest.Size(700, 500))
	app.Find("id:code-copy-icon")
	app.Press("role:button 复制代码")
	if app.Clipboard != "fmt.Println(\"hello\")" {
		t.Fatalf("code copied %q", app.Clipboard)
	}
	app.Find("id:code-copy-done")
	app.Screenshot("code-copy-feedback")
	app.Advance(2 * time.Second)
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:code-copy-done") })
}

func composerImageFixture(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *State, *downloadFixture) {
	t.Helper()
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 240, 160))
	for y := range 160 {
		for x := range 240 {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	f := &downloadFixture{demoAPI: newDemoAPI(), data: data.Bytes(), gate: make(chan struct{})}
	var s *State
	opts = append([]nsgotest.Option{nsgotest.Size(900, 700), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop})}, opts...)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, f)
		s.applyColors()
		s.uploads = []*Upload{{Name: "draft.png", MIME: "image/png", Data: f.data, Status: "ready"}}
		return core.View{Style: fill.Merge(core.Style{Padding: core.All(16)}), StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{s.uploadPreviews(cx), core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) })}}
	}, opts...)
	return app, s, f
}

func TestComposerImagePreviewAndIconDownload(t *testing.T) {
	app, _, f := composerImageFixture(t)
	preview, remove := app.Find("id:preview-upload-draft.png").Bounds(), app.Find("id:remove-upload-draft.png").Bounds()
	if remove.Origin.X >= preview.Origin.X+preview.Size.Width || remove.Origin.Y >= preview.Origin.Y+12 {
		t.Fatalf("remove action detached from thumbnail corner: preview=%+v remove=%+v", preview, remove)
	}
	app.Screenshot("draft-image-remove-corner")
	app.Press("id:preview-upload-draft.png")
	app.WaitFor("id:image-preview", time.Second)
	if f.reads.Load() != 0 {
		t.Fatal("draft preview tried fetching remote bytes")
	}
	if app.Find("id:preview-download").Props.Label != "下载图片" {
		t.Fatal("missing download label")
	}
	if app.Exists("text:下载") {
		t.Fatal("download still uses text")
	}
	app.Screenshot("draft-image-preview")
	if nsgotest.Target() == "" {
		app.Press("id:preview-download")
		app.WaitFor("id:attachment-download-save", time.Second)
		if len(app.Saves) != 1 || !bytes.Equal(app.Saves[0].Data, f.data) || f.reads.Load() != 0 {
			t.Fatal("draft download did not use local image bytes")
		}
		app.Press("id:attachment-download-cancel")
		app.WaitFor("id:image-preview", time.Second)
	}
	app.Press("id:preview-close")
	app.RightClick("id:preview-upload-draft.png")
	app.Press("id:upload-remove-action")
	app.WaitUntil(time.Second, func() bool { return !app.Exists("id:preview-upload-draft.png") })
	if app.Exists("id:image-preview") {
		t.Fatal("removing image also opened preview")
	}
}

func TestComposerImageContextualRemoval(t *testing.T) {
	if nsgotest.Target() == "macos" {
		t.Skip("native harness hover is unreliable; browser verifies pointer and keyboard disclosure")
	}
	for _, dark := range []bool{false, true} {
		t.Run(choose(dark, "dark", "light"), func(t *testing.T) {
			app, s, _ := composerImageFixture(t)
			app.Post(func() { s.setAppearance(choose(dark, "dark", "light")) })
			actions, preview, remove := "id:upload-actions-draft.png", "id:preview-upload-draft.png", "id:remove-upload-draft.png"
			if app.Find(actions).Style.Opacity != 0 {
				t.Fatal("image removal is visible at rest")
			}
			before := globalBounds(app.Find(preview))
			app.Hover(preview)
			app.WaitUntil(time.Second, func() bool { return app.Find(actions).Style.Opacity == 1 })
			app.Hover(remove)
			if app.Find(actions).Style.Opacity != 1 || globalBounds(app.Find(preview)) != before {
				t.Fatal("moving to removal hides the control or moves the thumbnail")
			}
			app.Screenshot("composer-image-remove-hover")
			app.HoverOut()
			app.WaitUntil(time.Second, func() bool { return app.Find(actions).Style.Opacity == 0 })
			app.Focus(preview)
			app.WaitUntil(time.Second, func() bool { return app.Find(actions).Style.Opacity == 1 })
			app.Key("Tab")
			app.Key("Enter")
			app.WaitUntil(time.Second, func() bool { return !app.Exists(preview) })
			if app.Exists("id:image-preview") || len(s.uploads) != 0 {
				t.Fatal("keyboard removal opened preview or kept the image")
			}
		})
	}
}

func TestComposerImageTouchRemoval(t *testing.T) {
	if nsgotest.Target() != "" {
		t.Skip("desktop targets do not emulate finger input")
	}
	app, s, _ := composerImageFixture(t, nsgotest.Size(390, 700), nsgotest.Environment(core.Environment{OS: core.OSAndroid, Idiom: core.IdiomPhone}))
	if app.Exists("id:remove-upload-draft.png") {
		t.Fatal("touch thumbnail shows a hover-only action")
	}
	app.LongPress("id:preview-upload-draft.png")
	app.WaitFor("id:upload-remove-action", time.Second)
	if app.Exists("id:image-preview") {
		t.Fatal("long press also opened preview")
	}
	app.Press("id:upload-remove-action")
	app.WaitUntil(time.Second, func() bool { return len(s.uploads) == 0 && !app.Exists("id:preview-upload-draft.png") })
}

func TestComposerUploadStatusLayout(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(choose(dark, "dark", "light"), func(t *testing.T) {
			app, s, f := composerImageFixture(t, nsgotest.Size(390, 500))
			app.Post(func() {
				s.cx.Post(func() {
					s.setAppearance(choose(dark, "dark", "light"))
					s.uploads = nil
					for _, status := range []string{"queued", "uploading", "verifying", "failed", "ready"} {
						sent := int64(len(f.data) / 2)
						if status == "queued" {
							sent = 0
						}
						if status == "verifying" {
							sent = int64(len(f.data))
						}
						s.uploads = append(s.uploads, &Upload{Name: status + ".png", MIME: "image/png", Data: f.data, Status: status, Sent: sent, Error: "Network unavailable"})
					}
					s.changed()
				})
			})
			app.WaitFor("id:retry-upload-failed.png", time.Second)
			if app.Exists("role:progressbar") || app.Exists("id:upload-progress-failed.png") {
				t.Fatal("upload footer duplicates progress and retry")
			}
			for _, name := range []string{"queued", "uploading", "verifying", "failed", "ready"} {
				card := globalBounds(app.Find("id:upload-" + name + ".png"))
				if card.Size.Width != 64 || card.Origin.X+card.Size.Width > 390 {
					t.Fatalf("upload card overflows or grows for status: %v", card)
				}
			}
			if app.Find("id:upload-progress-verifying.png").Text != "99%" {
				t.Fatal("upload completed before confirmation")
			}
			app.Screenshot("upload-cards-" + choose(dark, "dark", "light"))
		})
	}
}

func TestPinnedDragPersistsOrderWithoutChangingSelection(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		for i, ss := range s.sessions {
			s.pinned[sessionIdentity(ss)] = int64(len(s.sessions) - i)
		}
		s.catalogueVersion++
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 1000), nsgotest.Environment(core.Environment{Idiom: core.IdiomDesktop, OS: core.OSMacOS, Locale: "zh-CN"}))
	a, b := s.sessions[0], s.sessions[2]
	first, last := "id:"+sessionRowID("pinned", a), "id:"+sessionRowID("pinned", b)
	app.Type("id:chat-input", "keep draft")
	editor := app.Find("id:chat-input").ID
	app.Find(last)
	var delta core.Points
	app.Post(func() {
		a, _ := s.pinnedRefs[sessionIdentity(a)].Bounds()
		b, _ := s.pinnedRefs[sessionIdentity(b)].Bounds()
		delta = b.Origin.Y - a.Origin.Y
	})
	app.Settle()
	// Pan exercises the actual row callback; the native/browser drag checks
	// additionally verify gesture arbitration and pointer delivery.
	if nsgotest.Target() == "macos" || nsgotest.Target() == "web" {
		app.Drag(first, core.Point{Y: delta + 20})
	} else {
		app.Pan(first, core.Point{Y: delta + 20})
	}
	if s.pinnedOrder()[2] != sessionIdentity(a) {
		t.Fatal("drag did not move pin", s.pinnedOrder())
	}
	if app.Find("id:chat-input").ID != editor || app.Find("id:chat-input").Text != "keep draft" {
		t.Fatal("drag replaced composer")
	}
	app.Post(func() { s.loadSidebarPreferences(); s.catalogueVersion++; s.changed() })
	if s.pinnedOrder()[2] != sessionIdentity(a) {
		t.Fatal("order not persisted")
	}
	app.RightClick(first)
	app.Press("id:pin-move-up")
	if s.pinnedOrder()[1] != sessionIdentity(a) {
		t.Fatal("menu reorder failed")
	}
	app.Screenshot("pinned-reordered")
}
