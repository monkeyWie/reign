package hubui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/persistence"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// Each page is one completed turn. The middle and latest turns contain 49
// tool calls and one answer each, so two whole pages occupy only four rows.
type foldedHistoryAPI struct {
	*demoAPI
	reads       atomic.Int32
	fail        atomic.Bool
	toolPayload string
	dense       bool
	block       atomic.Bool
	canceled    atomic.Bool
}

func foldedPage(turn int) Object {
	var items []Object
	for i := range persistence.PageSize {
		item := Object{"id": fmt.Sprint(i), "role": "assistant", "text": fmt.Sprintf("Fixture reply %d", i)}
		if turn >= 3 && i < persistence.PageSize-1 {
			item["role"] = "tool"
		}
		items = append(items, item)
	}
	return Object{"id": fmt.Sprint(turn), "status": "completed", "items": items}
}
func (api *foldedHistoryAPI) ReadPage(ctx context.Context, _ persistence.Session, query persistence.PageQuery) (persistence.HistoryPage, error) {
	if query.Before == nil {
		return persistence.HistoryPage{}, nil
	}
	api.reads.Add(1)
	if api.block.Load() {
		<-ctx.Done()
		api.canceled.Store(true)
		return persistence.HistoryPage{}, ctx.Err()
	}
	if api.fail.Load() {
		return persistence.HistoryPage{}, errors.New("fixture read failed")
	}
	turn := query.Before.Turn - 1
	if query.After != nil {
		turn = query.After.Turn + 1
		if turn >= query.Before.Turn {
			return persistence.HistoryPage{Transcript: Object{}}, nil
		}
	}
	start := persistence.PageCursor{Turn: turn}
	turnData := foldedPage(turn)
	if api.dense {
		for _, item := range objects(turnData["items"]) {
			item["role"] = "tool"
		}
	}

	if turn == 3 && api.toolPayload != "" {
		objects(turnData["items"])[0]["activities"] = []Object{{"kind": "command", "detail": api.toolPayload}}
	}
	page := persistence.HistoryPage{Transcript: Object{"turns": []Object{turnData}}, Start: start, End: persistence.PageCursor{Turn: turn, Item: persistence.PageSize - 1}}
	if query.After != nil && turn+1 < query.Before.Turn {
		page.After = &page.End
	}
	if turn > 0 {
		page.Before = &start
	}
	return page, nil
}
func (*foldedHistoryAPI) ApplyHistory(context.Context, persistence.Session, persistence.HistoryPatch) error {
	return nil
}

func startFoldedHistory(t *testing.T, api *foldedHistoryAPI) (*nsgotest.App, *State) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, api)
		s.width.Set(1100)
		s.target, s.selected = "fixture", Object{"id": "example", "providerId": "fixture"}
		s.transcript = persistence.WithOrigin(Object{"turns": []Object{foldedPage(4)}}, persistence.PageCursor{Turn: 4}, true)
		s.rebuildMessages()
		return s.conversation(cx, false)
	}, nsgotest.Size(1100, 800))
	return app, s
}

func TestFoldedHistoryFillsViewportThenPagesOnScroll(t *testing.T) {
	api := &foldedHistoryAPI{demoAPI: newDemoAPI()}
	app, s := startFoldedHistory(t, api)
	list := "id:chat-message-list"
	app.WaitUntil(3*time.Second, func() bool {
		v := app.Find(list)
		return api.reads.Load() == 2 && v.Content.Height > v.Frame.Size.Height && math.Abs(float64(v.Content.Height-v.Frame.Size.Height-v.Offset.Y)) <= 1
	})
	if app.Exists("id:load-older") || app.Exists("id:load-newer") || app.Exists("id:jump-to-latest") || !app.Exists("id:message-4|49") {
		t.Fatal("filling the latest viewport entered history or hid the latest answer")
	}
	// A stream update after filling still changes the visible latest row.
	app.Post(func() {
		s.cx.Post(func() {
			turn := foldedPage(4)
			objects(turn["items"])[49]["text"] = "Updated fixture answer"
			s.transcript = persistence.WithOrigin(Object{"turns": []Object{turn}}, persistence.PageCursor{Turn: 4}, true)
			s.rebuildMessages()
			s.changed()
		})
	})
	app.WaitFor("text:Updated fixture answer", time.Second)
	if nsgotest.Target() != "" {
		app.Screenshot("filled-conversation-viewport")
	}
	if nsgotest.Target() != "" {
		app.Scroll(list, -app.Find(list).Offset.Y)
	} else {
		app.DragScroll(list, core.Point{Y: 40}, 0)
	}
	app.WaitUntil(3*time.Second, func() bool { return api.reads.Load() >= 3 && s.reading != nil })
	if nsgotest.Target() == "" {
		app.WaitFor("id:message-2|0", time.Second)
		at := globalBounds(app.Find("id:message-2|0")).Origin.Y - globalBounds(app.Find(list)).Origin.Y
		if math.Abs(float64(at+40)) > 1 {
			t.Fatalf("prepending moved reading row to %g, want -40", at)
		}
	}
	app.Press("id:jump-to-latest")
	app.WaitFor("text:Updated fixture answer", time.Second)
	if app.Exists("id:load-newer") {
		t.Fatal("latest retained a history footer")
	}
}

func TestFoldedHistoryFailureRetriesOnceOnDemand(t *testing.T) {
	api := &foldedHistoryAPI{demoAPI: newDemoAPI()}
	api.fail.Store(true)
	app, _ := startFoldedHistory(t, api)
	app.WaitFor("id:load-older", 3*time.Second)
	app.Advance(time.Second)
	if got := api.reads.Load(); got != 1 {
		t.Fatalf("failed page read %d times", got)
	}
	api.fail.Store(false)
	app.Press("id:load-older")
	app.WaitUntil(3*time.Second, func() bool {
		return api.reads.Load() == 3 && !app.Exists("id:load-older") && app.Find("id:chat-message-list").Offset.Y > 0
	})
}

func TestFoldedHistoryPayloadLimitKeepsUpwardScrollAvailable(t *testing.T) {
	api := &foldedHistoryAPI{demoAPI: newDemoAPI(), toolPayload: strings.Repeat("x", persistence.PageBytes)}
	app, s := startFoldedHistory(t, api)
	const list = "id:chat-message-list"
	app.WaitUntil(2*time.Second, func() bool {
		return api.reads.Load() == 1 && app.Find(list).Offset.Y > 0
	})
	if app.Exists("id:load-older") || app.Exists("id:load-newer") {
		t.Fatal("reaching the automatic read budget exposed a paging button")
	}
	app.Scroll(list, -24)
	app.WaitUntil(2*time.Second, func() bool { return api.reads.Load() >= 2 && s.reading != nil })
}

type scrollingHistoryAPI struct {
	*demoAPI
	persistence.HistoryDAO
}

func TestHistoryScrollRoundTrip(t *testing.T) {
	count := 1200
	// Platform drivers synthesize each wheel step in real time. Six pages
	// exercise eviction on both ends within their per-test timeout.
	if testing.Short() || nsgotest.Target() != "" {
		count = 300
	}
	ctx := context.Background()
	dao := timelineTestDAO(t)
	key := persistence.Session{Env: "fixture-" + persistence.Revision(), Provider: "demo", ID: "roundtrip"}
	defer dao.ForgetHistory(ctx, key.Env)
	items := make([]Object, count)
	for i := range items {
		items[i] = Object{"id": fmt.Sprint(i), "role": "assistant", "text": fmt.Sprintf("Fixture reply %d", i)}
	}
	history := Object{"turns": []Object{{"id": "t", "items": items}}}
	if err := dao.ApplyHistory(ctx, key, persistence.Diff("", nil, history)); err != nil {
		t.Fatal(err)
	}
	page, err := dao.ReadPage(ctx, key, persistence.PageQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, &scrollingHistoryAPI{newDemoAPI(), dao})
		s.width.Set(1100)
		s.target, s.selected = key.Env, Object{"id": key.ID, "providerId": key.Provider}
		s.transcript = page.View()
		s.rebuildMessages()
		return s.conversation(cx, false)
	}, nsgotest.Size(1100, 800))
	const list = "id:chat-message-list"
	app.WaitFor(fmt.Sprintf("id:message-t|%d", count-1), 3*time.Second)
	peakRows, peakPages := 0, 0
	check := func() {
		t.Helper()
		if app.Exists("id:load-newer") || app.Exists("id:load-older") {
			t.Fatal("scrolling exposed a pagination control")
		}
		rows := s.messageRows
		for i := 1; i < len(rows); i++ {
			prev, _ := strconv.Atoi(strings.TrimPrefix(rows[i-1].ID, "t|"))
			next, _ := strconv.Atoi(strings.TrimPrefix(rows[i].ID, "t|"))
			if next != prev+1 {
				t.Fatalf("noncontiguous records %s %s", rows[i-1].ID, rows[i].ID)
			}
		}
		peakRows = max(peakRows, len(rows))
		peakPages = max(peakPages, len(s.readingPages))
		if len(s.readingPages) > historyWindowPages || s.reading != nil && len(rows) > persistence.WindowSize {
			t.Fatalf("unbounded reading window: %d rows in %d pages", len(rows), len(s.readingPages))
		}
	}
	edge := func(up bool) {
		t.Helper()
		start := s.historyEdge()
		for i := 0; i < 32; i++ {
			v := app.Find(list)
			to := v.Content.Height - v.Frame.Size.Height
			if up {
				to = 0
			}
			delta := to - v.Offset.Y
			if nsgotest.Target() == "" {
				app.DragScroll(list, core.Point{Y: to}, 0)
				return
			}
			if math.Abs(float64(delta)) < 1 || s.historyEdge() != start {
				return
			}
			// The browser test driver animates the requested distance a wheel
			// step at a time. Keep each individual input below its RPC timeout.
			app.Scroll(list, max(-1800, min(1800, delta)))
		}
		t.Fatal("scroll did not reach the window edge")
	}
	for n := 0; s.historyEdge().local; n++ {
		if n > count/persistence.PageSize+2 {
			t.Fatal("older reads stalled")
		}
		start := s.historyEdge().before
		edge(true)
		app.WaitUntil(3*time.Second, func() bool { return s.historyEdge().before.Less(start) })
		check()
	}
	edge(true)
	app.WaitFor("id:message-t|0", 3*time.Second)
	// Incoming content belongs to the live projection while the oldest row
	// stays visible. The forward seam must use this updated live tail.
	app.Post(func() {
		turn := copyObject(objects(s.transcript["turns"])[0])
		newItem := Object{"id": fmt.Sprint(count), "role": "assistant", "text": "Newest fixture reply"}
		turn["items"] = append(append([]Object{}, objects(turn["items"])...), newItem)
		live := copyObject(s.transcript)
		live["turns"] = []Object{turn}
		s.transcript = live
		s.rebuildMessages()
		s.changed()
	})
	app.WaitFor("id:message-t|0", time.Second)
	for n := 0; s.reading != nil; n++ {
		if n > count/persistence.PageSize+2 {
			t.Fatal("newer reads stalled")
		}
		end := s.historyEdge().after
		edge(false)
		app.WaitUntil(3*time.Second, func() bool { return s.reading == nil || end.Less(s.historyEdge().after) })
		check()
	}
	edge(false)
	app.WaitFor("text:Newest fixture reply", 3*time.Second)
	app.WaitForGone("id:jump-to-latest", 3*time.Second)
	t.Logf("%d stored messages plus one live arrival, peak %d projected rows and %d history pages", count, peakRows, peakPages)
}

func TestHiddenHistoryEndsAtLastReadablePage(t *testing.T) {
	api := &foldedHistoryAPI{demoAPI: newDemoAPI(), dense: true}
	app, s := startFoldedHistory(t, api)
	app.WaitUntil(2*time.Second, func() bool { return api.reads.Load() == 4 && !s.historyEdge().local })
	v := app.Find("id:chat-message-list")
	if v.Content.Height > v.Frame.Size.Height+1 || s.reading != nil || app.Exists("id:load-older") {
		t.Fatal("hidden history created an empty scroll range")
	}
	app.WaitFor("id:message-4|49", time.Second)
}

func TestReversingScrollCancelsSlowPage(t *testing.T) {
	api := &foldedHistoryAPI{demoAPI: newDemoAPI()}
	app, s := startFoldedHistory(t, api)
	const list = "id:chat-message-list"
	app.WaitUntil(time.Second, func() bool { return api.reads.Load() == 2 && app.Find(list).Offset.Y > 0 })
	api.block.Store(true)
	app.DragScroll(list, core.Point{Y: 20}, 0)
	app.WaitUntil(time.Second, func() bool { return api.reads.Load() == 3 })
	v := app.Find(list)
	app.DragScroll(list, core.Point{Y: v.Content.Height - v.Frame.Size.Height}, 0)
	app.WaitUntil(time.Second, func() bool { return api.canceled.Load() && !s.actionPending("history:"+s.key()) })
	if s.reading != nil || app.Exists("id:load-older") || !app.Exists("id:message-4|49") {
		t.Fatal("obsolete read displaced the live tail")
	}
}

// Hidden outputs can occupy more raw pages than the entire reading window.
func TestHistoryScrollAcrossHiddenPages(t *testing.T) {
	ctx := context.Background()
	dao := timelineTestDAO(t)
	key := persistence.Session{Env: "fixture-" + persistence.Revision(), Provider: "demo", ID: "sparse"}
	defer dao.ForgetHistory(ctx, key.Env)
	var items []Object
	for i := range 400 {
		item := Object{"id": fmt.Sprint(i), "role": "assistant", "text": fmt.Sprintf("Visible fixture reply %d", i)}
		if i >= 30 && i < 370 && i != 124 && i != 201 {
			item["role"], item["type"] = "tool", "functionCallOutput"
			if i >= 362 {
				item["type"] = "commandExecution"
			}
		} else if i%13 == 0 {
			item["text"] = strings.Repeat("A longer fixture paragraph with measured Markdown.\n\n", 14)
		}
		items = append(items, item)
	}
	history := Object{"turns": []Object{{"id": "t", "status": "completed", "items": items}}}
	if err := dao.ApplyHistory(ctx, key, persistence.Diff("", nil, history)); err != nil {
		t.Fatal(err)
	}
	page, err := dao.ReadPage(ctx, key, persistence.PageQuery{Limit: persistence.WindowSize})
	if err != nil {
		t.Fatal(err)
	}
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newState(cx, &scrollingHistoryAPI{newDemoAPI(), dao})
		s.width.Set(1100)
		s.target, s.selected = key.Env, Object{"id": key.ID, "providerId": key.Provider}
		s.transcript = page.View()
		s.rebuildMessages()
		return s.conversation(cx, false)
	}, nsgotest.Size(1100, 800))
	const list = "id:chat-message-list"
	app.WaitFor("id:message-t|399", 3*time.Second)
	for _, target := range []int{0, 399} {
		seen := map[string]bool{}
		for _, row := range s.messageRows {
			seen[row.ID] = true
		}
		for attempt := 0; ; attempt++ {
			v := app.Find(list)
			to := v.Content.Height - v.Frame.Size.Height
			if target == 0 {
				to = 0
			}
			if delta := to - v.Offset.Y; math.Abs(float64(delta)) >= 1 {
				if nsgotest.Target() == "" {
					app.DragScroll(list, core.Point{Y: to}, 0)
				} else {
					// The browser wheel driver emits 20-point events synchronously.
					app.Scroll(list, min(720, max(-720, delta)))
				}
			}
			app.WaitUntil(3*time.Second, func() bool { return !s.actionPending("history:" + s.key()) })
			if len(s.messageRows) == 0 {
				t.Fatal("scroll replaced the visible transcript with an empty page")
			}
			for _, row := range s.messageRows {
				seen[row.ID] = true
			}
			if len(s.readingPages) > historyWindowPages {
				t.Fatalf("retained %d reading pages", len(s.readingPages))
			}
			v = app.Find(list)
			var top, bottom bool
			var checkRows func(*nsgotest.View)
			checkRows = func(node *nsgotest.View) {
				if strings.HasPrefix(node.Props.TestID, "message-t|") {
					b := globalBounds(node)
					y := b.Origin.Y - globalBounds(v).Origin.Y
					top = top || y <= 32 && y+b.Size.Height >= 32
					bottom = bottom || y <= v.Frame.Size.Height-32 && y+b.Size.Height >= v.Frame.Size.Height-32
					return
				}
				for _, child := range node.Children {
					checkRows(child)
				}
			}
			checkRows(v)
			if !top || !bottom {
				t.Fatalf("visible messages do not fill the viewport: top=%t bottom=%t edge=%+v", top, bottom, s.historyEdge())
			}
			boundary := target == 0 && !s.historyEdge().local || target == 399 && s.reading == nil
			if boundary && app.Exists(fmt.Sprintf("id:message-t|%d", target)) {
				row := globalBounds(app.Find(fmt.Sprintf("id:message-t|%d", target)))
				y := row.Origin.Y - globalBounds(v).Origin.Y
				if y < v.Frame.Size.Height && y+row.Size.Height > 0 {
					if !seen["t|124"] || !seen["t|201"] {
						t.Fatalf("scroll to %d skipped a readable reply in the hidden run", target)
					}
					break
				}
			}
			if attempt >= 30 {
				t.Fatalf("scroll could not reach %d", target)
			}
		}
	}
}

// A run of tool-only database pages belongs to the following answer's
// disclosure. A raw output is private, while each activity remains readable.
func TestFoldedProcessKeepsDetailsAcrossPhysicalPages(t *testing.T) {
	ctx := context.Background()
	dao := timelineTestDAO(t)
	route := persistence.Session{Env: "fixture-" + persistence.Revision(), Provider: "demo", ID: "process"}
	defer dao.ForgetHistory(ctx, route.Env)
	var items []Object
	const count = 170
	for i := range count {
		items = append(items, Object{"id": fmt.Sprint(i), "role": "tool", "type": "commandExecution", "text": "private raw output", "activities": []Object{{"kind": "command", "detail": fmt.Sprint(i)}}})
	}
	answer := Object{"id": "final", "role": "assistant", "text": "Fixture answer"}
	turn := Object{"id": "turn", "status": "completed", "items": append(items, answer)}
	if err := dao.ApplyHistory(ctx, route, persistence.Diff("", nil, Object{"turns": []Object{turn}})); err != nil {
		t.Fatal(err)
	}
	live := Object{"turns": []Object{{"id": "turn", "status": "completed", "items": []Object{answer}}}}
	before := persistence.PageCursor{Turn: 0, Item: count}
	p, err := readDisplayedPage(ctx, dao, route, persistence.PageQuery{Before: &before}, live)
	if err != nil {
		t.Fatal(err)
	}
	rows := messages(joinPages(p.Transcript, live, persistence.PageSize))
	if len(rows) != 2 || rows[0].Role != "work" || len(rows[0].Details) != 1 {
		t.Fatal("lost work disclosure")
	}
	tools := rows[0].Details[0].Tools
	if len(tools) != count {
		t.Fatalf("retained %d/%d activity details", len(tools), count)
	}
	for i, tool := range tools {
		if tool["text"] != nil || str(objects(tool["activities"])[0], "detail") != fmt.Sprint(i) {
			t.Fatalf("incorrect activity detail at %d", i)
		}
	}
}
