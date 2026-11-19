package hubui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type switchingSessionAPI struct {
	*demoAPI
	histories map[string]Object
}

func (f *switchingSessionAPI) Call(ctx context.Context, method string, p Object, target string, r Object) (any, error) {
	if (method == "client.call" || method == "client.cached") && str(r, "method") == "transcript" {
		data, err := json.Marshal(f.histories[str(r, "session")])
		if err != nil {
			return nil, err
		}
		var result Object
		err = json.Unmarshal(data, &result)
		return result, err
	}
	if method == "client.list" {
		return []Object{{"id": "demo-device", "name": "Windows fixture", "platform": "windows", "connected": true}}, nil
	}
	return f.demoAPI.Call(ctx, method, p, target, r)
}

func TestRapidSessionSwitching(t *testing.T) {
	f := &switchingSessionAPI{demoAPI: newDemoAPI(), histories: map[string]Object{}}
	f.sessions = nil
	counts := []int{32, 1, 8}
	for index, count := range counts {
		id := fmt.Sprintf("switch-%d", index)
		session := Object{"id": id, "providerId": "codex-app", "title": id, "cwd": `C:\fixture`, "createdAt": index + 1}
		f.sessions = append(f.sessions, session)
		turns := []Object{}
		for turn := range count {
			turns = append(turns, Object{"id": fmt.Sprintf("%s-%d", id, turn), "status": "completed", "items": []Object{
				{"id": "user", "role": "user", "text": fmt.Sprintf("Fixture input %d", turn)},
				{"id": "answer", "role": "assistant", "text": "## Fixture answer\n\n" + strings.Repeat("Body paragraph.\n\n", index*3+1) + "```go\nfunc example() {}\n```"},
			}})
		}
		f.histories[id] = Object{"session": session, "turns": turns}
	}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return RootWithAPI(cx, f, true)
	}, nsgotest.Size(1320, 880), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	for step := range 30 {
		id := fmt.Sprintf("switch-%d", step%3)
		selector := sessionSelector("demo-device", "codex-app", id)
		app.WaitFor(selector, 3*time.Second)
		app.Press(selector)
		app.WaitFor("id:chat-input", time.Second)
		app.WaitFor(fmt.Sprintf("id:message-%s-%d|answer", id, counts[step%3]-1), 3*time.Second)
		app.Focus("id:chat-input")
		if counts[step%3] > 1 {
			app.Scroll("id:chat-message-list", -180)
		}
		app.Settle()
	}
}

func TestDetachedConversationScrollUpdates(t *testing.T) {
	var shown, content *core.Signal[bool]
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		cx.SetTheme(core.DefaultTheme())
		installScrollbars(cx)
		shown, content = core.NewSignal(cx, true), core.NewSignal(cx, false)
		held := core.NewHolder(cx, func(cx *core.Scope) core.Node {
			return core.Show{When: content.Get, Then: func(*core.Scope) core.Node {
				return core.Scroll{TestID: "held-conversation-scroll", Style: core.Style{Height: core.Pt(120)}, Children: []core.Node{
					core.View{Style: core.Style{Position: core.Sticky, Top: core.Pt(0), Height: core.Pt(24)}, Children: []core.Node{core.Text{Content: "Fixture header"}}},
					core.View{Style: core.Style{Height: core.Pt(800)}},
				}}
			}}
		})
		return core.Show{When: shown.Get, Then: func(*core.Scope) core.Node { return held.Place() }}
	}, nsgotest.Size(400, 300))
	// Retained content can finish building after its conversation has left the
	// mounted tree. Native sticky updates must not target a missing scroll view.
	app.Post(func() { shown.Set(false) })
	app.Settle()
	app.Post(func() { content.Set(true) })
	app.Settle()
	if app.Exists("id:held-conversation-scroll") {
		t.Fatal("detached content became visible")
	}
	app.Post(func() { shown.Set(true) })
	app.WaitFor("id:held-conversation-scroll", time.Second)
	app.Scroll("id:held-conversation-scroll", 100)
}
