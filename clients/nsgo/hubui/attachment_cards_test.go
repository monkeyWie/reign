package hubui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func attachmentCardsFixture(t *testing.T, dark bool) (*nsgotest.App, *State, *downloadFixture) {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	for y := range 60 {
		for x := range 80 {
			img.Set(x, y, color.RGBA{R: 30, G: 140, B: 100, A: 255})
		}
	}
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	f := &downloadFixture{demoAPI: newDemoAPI(), data: buffer.Bytes(), gate: make(chan struct{})}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s = newState(cx, f)
		s.applyColors()
		s.target, s.selected = "fixture", Object{"id": "session", "providerId": "codex-app"}
		attachments := []Object{}
		for i := range 5 {
			a := f.attachment()
			a["id"], a["source"], a["kind"] = fmt.Sprint(i), fmt.Sprintf("attachment://fixture/%d", i), "file"
			a["name"] = strings.Repeat("long-filename-", 12) + ".apk"
			if i < 2 {
				a["kind"], a["name"] = "image", fmt.Sprintf("photo-%d.png", i)
				s.images[s.key()+"|"+str(a, "id")+"|false"] = f.data
			}
			if i == 4 {
				a["inline"] = true
			}
			attachments = append(attachments, a)
		}
		m := Message{ID: "cards", Role: "assistant", Text: "Attachments", Attachments: attachments}
		// The fixture parent accepts clicks so browser tests can exercise a
		// real pointer activation on otherwise noninteractive file content.
		return core.View{Style: fill, OnPress: func(core.PressEvent) {}, StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{
			s.message(cx, func() Message { return m }),
			core.Dynamic(func(cx *core.Scope) core.Node { return s.dialogView(cx, s.dialog.Get()) }),
		}}
	}, nsgotest.Size(820, 900), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Dark: dark}))
	return app, s, f
}

func TestAttachmentCardsSeparatePreviewDownloadAndUnsupportedFiles(t *testing.T) {
	app, s, f := attachmentCardsFixture(t, false)
	app.WaitFor("id:download-4", time.Second) // Inline files still have a download action.
	app.Press("id:attachment-name-2")
	app.Settle()
	if app.Exists("id:image-preview") || app.Exists("id:attachment-download") || f.reads.Load() != 0 {
		t.Fatal("unsupported file activation started preview or download")
	}
	if app.Find("id:attachment-2").Props.Role == core.RoleButton {
		t.Fatal("unsupported preview advertised an interactive card")
	}
	app.Press("id:attachment-name-0")
	app.WaitFor("id:image-preview", time.Second)
	if app.Exists("id:attachment-download") || f.reads.Load() != 0 {
		t.Fatal("image preview started a download")
	}
	app.Press("id:preview-close")
	for _, id := range []string{"0", "2", "4"} {
		app.Press("id:download-" + id)
		app.WaitFor("id:attachment-download", time.Second)
		if app.Exists("id:image-preview") {
			t.Fatal("download activation also opened image preview")
		}
		app.Press("id:attachment-download-cancel")
	}
	app.Post(func() {
		s.cx.Post(func() {
			s.openLink(s.cx, Message{Attachments: []Object{{"id": "2", "kind": "file", "source": "attachment://file"}}}, "attachment://file")
		})
	})
	app.Settle()
	if app.Exists("id:attachment-download") {
		t.Fatal("an attachment link bypassed the download icon")
	}
}

func TestAttachmentCardsResponsiveGridLight(t *testing.T) { testAttachmentCardsGrid(t, false) }
func TestAttachmentCardsResponsiveGridDark(t *testing.T)  { testAttachmentCardsGrid(t, true) }

func testAttachmentCardsGrid(t *testing.T, dark bool) {
	app, _, _ := attachmentCardsFixture(t, dark)
	imageID := app.Find("role:image photo-0.png").ID
	cardID := app.Find("id:attachment-0").ID
	for step, width := range []core.Points{820, 564, 563, 372, 371, 320, 170, 820} {
		if nsgotest.Target() == "" || nsgotest.Target() == "web" {
			app.Resize(width, 900)
		}
		app.Settle()
		grid := globalBounds(app.Find("id:attachments-cards"))
		columns := max(1, int((grid.Size.Width+attachmentCardGap)/(attachmentCardMinimum+attachmentCardGap)))
		first := globalBounds(app.Find("id:attachment-0"))
		for i := range 5 {
			card := globalBounds(app.Find("id:attachment-" + fmt.Sprint(i)))
			download := globalBounds(app.Find("id:download-" + fmt.Sprint(i)))
			if card.Size.Width <= 0 || card.Origin.X < grid.Origin.X-.5 || card.Origin.X+card.Size.Width > grid.Origin.X+grid.Size.Width+.5 {
				t.Fatalf("card overflow at width %g: grid=%v card=%v", width, grid, card)
			}
			if math.Abs(float64(card.Size.Width-first.Size.Width)) > 1 {
				t.Fatalf("unequal card columns at width %g", width)
			}
			if i < columns && math.Abs(float64(card.Origin.Y-first.Origin.Y)) > 1 || i == columns && card.Origin.Y <= first.Origin.Y {
				t.Fatalf("incorrect column transition at width %g: columns=%d card=%v", width, columns, card)
			}
			if download.Origin.X < card.Origin.X || download.Origin.X+download.Size.Width > card.Origin.X+card.Size.Width+.5 {
				t.Fatalf("download control clipped at width %g", width)
			}
		}
		if app.Find("id:attachment-0").ID != cardID || app.Find("role:image photo-0.png").ID != imageID {
			t.Fatal("resizing remounted the attachment or decoded image")
		}
		if width == 320 || step == 0 {
			app.Screenshot(fmt.Sprintf("attachment-cards-%g-dark-%t", width, dark))
		}
	}
}
