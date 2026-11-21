package hubui

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/composerdraft"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestComposerFollowsExternalModelWithoutDiscardingLocalChoice(t *testing.T) {
	var s *State
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = workspaceLayoutFixture(cx)
		s.models = []Object{{"id": "model-a", "name": "Model A"}, {"id": "model-b", "name": "Model B"}, {"id": "model-c", "name": "Model C"}}
		s.setTranscript(Object{"session": Object{"id": str(s.selected, "id"), "providerId": str(s.selected, "providerId"), "model": "model-a"}})
		return s.workspaceView(cx)
	}, nsgotest.Size(1100, 820))
	update := func(model string) {
		app.Post(func() {
			s.setTranscript(Object{"session": Object{"id": str(s.selected, "id"), "providerId": str(s.selected, "providerId"), "model": model}})
		})
		app.Settle()
	}
	app.Type("id:chat-input", "Keep the unsent draft")
	editor := app.Find("id:chat-input").ID
	update("model-b")
	app.WaitFor("text:Model B", time.Second)
	app.Press("id:model-selector")
	app.Press("role:button Model C")
	update("model-b") // Replaying unchanged metadata must not undo a local choice.
	app.Find("text:Model C")
	update("") // Unknown metadata is not a model change.
	app.Find("text:Model C")
	update("model-a") // A later external change wins.
	app.Find("text:Model A")
	if app.Find("id:chat-input").ID != editor || app.Find("id:chat-input").Text != "Keep the unsent draft" {
		t.Fatal("model synchronization replaced the editor or its draft")
	}
}

func TestComposerModelDraftRestorationOrder(t *testing.T) {
	for _, sourceFirst := range []bool{false, true} {
		for _, current := range []string{"model-a", "model-c"} {
			t.Run(current+"/sourceFirst="+fmt.Sprint(sourceFirst), func(t *testing.T) {
				store := &draftMemory{values: map[string]json.RawMessage{}}
				api := &persistentDemo{newDemoAPI(), store}
				var s *State
				app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
					s = workspaceLayoutFixture(cx)
					s.api = api
					s.draftWriter = newDraftWriter(s.ctx, api, func(error) {})
					s.draftLoads = map[string]string{s.key(): "ready"}
					s.modelBase, s.model = "model-a", "model-b"
					s.draft.Set("Unsent text")
					s.persistDraft()
					return s.conversation(cx, false)
				}, nsgotest.Size(1100, 820))
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := s.draftWriter.flush(ctx); err != nil {
					t.Fatal(err)
				}
				v, err := composerdraft.Call(ctx, store, "client.draft.get", s.target, Object{"provider": str(s.selected, "providerId"), "session": str(s.selected, "id")})
				if err != nil || v.(composerdraft.Draft).ModelBase != "model-a" {
					t.Fatal("draft lost the model's source baseline", v, err)
				}
				app.Post(func() {
					s.model, s.modelBase = "", ""
					s.draft.Set("")
					delete(s.draftLoads, s.key())
					if sourceFirst {
						s.syncModel(current)
					}
					s.loadDraft()
				})
				app.WaitUntil(time.Second, func() bool { return s.draftReady() })
				if !sourceFirst {
					app.Post(func() { s.syncModel(current); s.changed() })
					app.Settle()
				}
				want := "model-b"
				if current == "model-c" {
					want = current
				}
				if s.model != want || app.Find("id:chat-input").Text != "Unsent text" {
					t.Fatalf("restoration overwrote the newer model or draft: model=%q want=%q", s.model, want)
				}
			})
		}
	}
}
