package hubui

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"golang.org/x/sys/windows"
)

func TestWindowsCaptionHitWithTransformedContent(t *testing.T) {
	if nsgotest.Target() != "windows" {
		t.Skip("requires the isolated native Windows UI harness")
	}
	title := fmt.Sprintf("reignhub-caption-fixture-%d", time.Now().UnixNano())
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		desktop.SetTitle(cx, title)
		s := newState(cx, newDemoAPI())
		return s.windowFrame(cx, core.View{Style: fill, Children: []core.Node{
			core.View{Style: core.Style{Width: core.Pt(30), Height: core.Pt(30), Transform: core.Transform{Rotate: 45}}},
		}})
	}, nsgotest.Size(1000, 700))
	user32 := windows.NewLazySystemDLL("user32.dll")
	name, err := windows.UTF16PtrFromString(title)
	if err != nil {
		t.Fatal(err)
	}
	hwnd, _, err := user32.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(name)))
	if hwnd == 0 {
		t.Fatalf("isolated fixture window not found: %v", err)
	}
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		t.Fatal("fixture DPI unavailable")
	}
	for _, item := range []struct {
		id   string
		want uintptr
	}{
		{"content-toolbar-slot", 2},                  // HTCAPTION: system drag/double-click behavior.
		{"toggle-sidebar", 1}, {"connect-device", 1}, // HTCLIENT: ordinary actions.
	} {
		bounds := globalBounds(app.Find("id:" + item.id))
		p := struct{ x, y int32 }{int32((bounds.Origin.X + bounds.Size.Width/2) * core.Points(dpi) / 96), int32((bounds.Origin.Y + bounds.Size.Height/2) * core.Points(dpi) / 96)}
		if ok, _, err := user32.NewProc("ClientToScreen").Call(hwnd, uintptr(unsafe.Pointer(&p))); ok == 0 {
			t.Fatal(err)
		}
		at := uintptr(uint32(uint16(p.x)) | uint32(uint16(p.y))<<16)
		var result uintptr
		ok, _, err := user32.NewProc("SendMessageTimeoutW").Call(hwnd, 0x0084, 0, at, 2, 1000, uintptr(unsafe.Pointer(&result)))
		if ok == 0 {
			t.Fatalf("caption hit query: %v", err)
		}
		if result != item.want {
			t.Fatalf("%s: native hit %d, want %d", item.id, result, item.want)
		}
	}
}
