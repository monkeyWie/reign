package hubui

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestScrollbarsOnlyAppearWhileScrolling(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		installScrollbars(cx)
		return core.Scroll{TestID: "scrollbar-fixture", Style: fill.Merge(core.Style{Background: core.RGB(0xffffff)}), Children: []core.Node{core.View{Style: core.Style{Height: core.Pt(3000)}}}}
	}, nsgotest.Size(390, 600))
	bar := func() *nsgotest.View { return app.FindMounted("label:垂直滚动条") }
	if bar().Props.PointerEvents != core.PointerEventsNone {
		t.Fatal("idle scroll bar was visible at startup")
	}
	if !app.Find("id:scrollbar-fixture").Scroll.HideIndicator || bar().Frame.Size.Width != 8 {
		t.Fatal("native scroll indicator was not replaced by the slim bar")
	}
	app.Scroll("id:scrollbar-fixture", 120)
	app.Advance(150 * time.Millisecond)
	app.Screenshot("slim-scrollbar-active")
	if bar().Props.PointerEvents != core.PointerEventsAuto {
		t.Fatal("scrolling did not reveal the bar")
	}
	app.Advance(time.Second)
	if bar().Props.PointerEvents != core.PointerEventsNone {
		t.Fatal("scroll bar did not disappear after scrolling stopped")
	}
	app.Screenshot("slim-scrollbar-idle")
}
