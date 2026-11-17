package hubui

import (
	"fmt"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestActivityHoverLight(t *testing.T) { testActivityHover(t, false) }
func TestActivityHoverDark(t *testing.T)  { testActivityHover(t, true) }

func testActivityHover(t *testing.T, dark bool) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		s := newState(cx, newDemoAPI())
		s.applyColors()
		s.devices, s.target = []Object{{"id": "fixture", "connected": true}}, "fixture"
		m := Message{Role: "activity", Live: true, Tools: []Object{{"id": "tool", "role": "tool", "status": "inProgress", "activities": []Object{{"kind": "read", "title": "读取文件", "detail": "fixture.go"}}}}}
		return core.View{Style: fill.Merge(core.Style{Padding: core.All(24)}), StyleFunc: func() core.Style { return core.Style{Background: palette(cx).background} }, Children: []core.Node{s.activity(cx, func() Message { return m })}}
	}, nsgotest.Size(600, 300), nsgotest.Environment(core.Environment{Dark: dark}))
	before := app.Query("text:正在读取文件")[0]
	color, id := before.TextStyle.Color, before.ID
	background := app.Find("id:tool-group-toggle").Style.Background
	app.Screenshot(fmt.Sprintf("activity-idle-dark-%t", dark))
	app.Hover("id:tool-group-toggle")
	after := app.Query("text:正在读取文件")[0]
	if after.TextStyle.Color == color || after.ID != id {
		t.Fatal("hover must illuminate the existing shimmer text")
	}
	if app.Find("id:tool-group-toggle").Style.Background != background {
		t.Fatal("hover introduced a row background")
	}
	app.Screenshot(fmt.Sprintf("activity-hover-dark-%t", dark))
	app.Press("id:tool-group-toggle")
	app.Find("text:fixture.go")
}
