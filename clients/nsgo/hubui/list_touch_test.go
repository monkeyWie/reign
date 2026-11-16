package hubui

import (
	"fmt"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestTouchingClickableListStopsMomentumWithoutOpeningRow(t *testing.T) {
	presses := 0
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		brake := &listTouchBrake{enabled: true}
		return brake.wrap(core.List{TestID: "list", Style: core.Style{Height: core.Pt(200)}, Count: func() int { return 20 }, EstimatedSize: 60,
			Row: func(cx *core.Scope, r core.ListRow) core.Node {
				return core.View{TestID: fmt.Sprintf("row-%d", r.Index()), Role: core.RoleButton, OnPress: func(core.PressEvent) { brake.action(func() { presses++ })() }, Style: core.Style{Height: core.Pt(60)}, Children: []core.Node{core.Text{Content: "会话"}}}
			}})
	}, nsgotest.Size(390, 200), nsgotest.Environment(core.Environment{OS: core.OSAndroid}))
	app.ScrollTo("id:list", core.Point{Y: 120})
	list := app.Find("id:list")
	before := list.Applied
	app.Report(nsgotest.Event{Kind: nsgotest.EventMomentumStart, ID: list.ID, Point: list.Offset, Value: 1, Count: list.Applied})
	finger := app.Touch("id:row-2", core.Point{X: 20, Y: 10})
	finger.Lift()
	list = app.Find("id:list")
	if list.Applied <= before || list.Offset.Y != 120 || presses != 0 {
		t.Fatalf("braking touch moved/opened row or failed to stop native motion: offset=%v commands=%d/%d presses=%d", list.Offset, list.Applied, before, presses)
	}
	// A fresh tap after braking is a normal activation.
	app.Touch("id:row-2", core.Point{X: 20, Y: 10}).Lift()
	if presses != 1 {
		t.Fatalf("braking suppressed the next tap: presses=%d", presses)
	}
	// A drag that is taken by the native scroll view must not poison later clicks.
	list = app.Find("id:list")
	app.Report(nsgotest.Event{Kind: nsgotest.EventMomentumStart, ID: list.ID, Point: list.Offset, Value: 1, Count: list.Applied})
	finger = app.Touch("id:row-2", core.Point{X: 20, Y: 30})
	finger.Move(core.Point{X: 20, Y: 5})
	finger.Lift()
	app.Touch("id:row-3", core.Point{X: 20, Y: 10}).Lift()
	if presses != 2 {
		t.Fatalf("drag/cancel left row interaction blocked: presses=%d", presses)
	}
}
