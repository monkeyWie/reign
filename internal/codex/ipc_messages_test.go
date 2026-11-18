package codex

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
)

func TestIPCCompletionPublishesFullAnswerBeforeTerminalStatus(t *testing.T) {
	for _, version := range []int{11, 13} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
			b := newAppIPC(p)
			defer b.cancel()
			p.ipc = b
			b.followed["s"] = &followedThread{owner: "app", changed: make(chan struct{})}
			state := map[string]any{"id": "s", "turns": []any{map[string]any{"turnId": "t", "status": "inProgress", "items": []any{
				map[string]any{"id": "progress", "type": "agentMessage", "phase": "commentary", "text": "Working"},
				map[string]any{"id": "answer", "type": "agentMessage", "phase": "final_answer", "text": "Ready"},
			}}}}
			broadcast := func(change any) {
				b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Source: "app", Version: version, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": change})})
			}
			broadcast(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state})
			epoch, cursor := p.stream().log.Position()
			broadcast(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{
				map[string]any{"op": "replace", "path": []any{"turns", 0, "status"}, "value": "completed"},
				map[string]any{"op": "add", "path": []any{"turns", 0, "durationMs"}, "value": 135123},
				map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 1, "text"}, "value": "Ready to use"},
			}})
			batch, err := p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			if err != nil {
				t.Fatal(err)
			}
			answer, terminal := -1, -1
			for i, event := range batch.Events {
				if event.ItemID == "answer" && event.Kind == "item" && event.Item.Text == "Ready to use" && event.Item.Phase == "final_answer" {
					answer = i
				}
				if event.Kind == "turn" && event.Status == "completed" {
					terminal = i
					if event.DurationMS == nil || *event.DurationMS != 135123 {
						t.Fatal("completion lost elapsed time")
					}
				}
			}
			if answer < 0 || terminal <= answer {
				t.Fatalf("final answer was sent as an ignorable late delta: %+v", batch.Events)
			}
			epoch, cursor = p.stream().log.Position()
			broadcast(map[string]any{"type": "patches", "baseRevision": 2, "revision": 3, "patches": []any{
				map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 1, "text"}, "value": "Ready to use now"},
			}})
			batch, err = p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			if err != nil || len(batch.Events) != 1 || batch.Events[0].Kind != "item" || batch.Events[0].Item.Text != "Ready to use now" {
				t.Fatalf("late terminal correction was not authoritative: %+v %v", batch, err)
			}
			epoch, cursor = p.stream().log.Position()
			broadcast(map[string]any{"type": "patches", "baseRevision": 3, "revision": 4, "patches": []any{
				map[string]any{"op": "replace", "path": []any{"turns", 0, "durationMs"}, "value": 136000},
			}})
			batch, err = p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			if err != nil || len(batch.Events) != 1 || batch.Events[0].Kind != "turn" || batch.Events[0].DurationMS == nil || *batch.Events[0].DurationMS != 136000 {
				t.Fatalf("duration-only correction was lost: %+v %v", batch, err)
			}
			out, err := p.Transcript(context.Background(), "s", "")
			if err != nil || len(out.Turns) != 1 || len(out.Turns[0].Items) != 2 || out.Turns[0].Items[0].Phase != "commentary" || out.Turns[0].Items[1].Phase != "final_answer" || out.Turns[0].Items[1].Text != "Ready to use now" {
				t.Fatalf("history lost the message phases or final content: %+v %v", out, err)
			}
			if out.Turns[0].DurationMS == nil || *out.Turns[0].DurationMS != 136000 {
				t.Fatal("transcript duration differs from live completion")
			}
		})
	}
}

func TestIPCSteeringMessagesSurviveHistoryMergeAndAcknowledgement(t *testing.T) {
	for _, version := range []int{11, 13} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
			b := newAppIPC(p)
			defer b.cancel()
			p.ipc = b
			b.followed["s"] = &followedThread{owner: "app", changed: make(chan struct{})}
			u := map[string]any{"id": "u", "type": "userMessage", "content": []any{map[string]any{"type": "text", "text": "first"}}}
			a := map[string]any{"id": "a", "type": "agentMessage", "text": "reply"}
			state := map[string]any{"id": "s", "historyMode": "complete", "turnHistory": map[string]any{"kind": "canonical", "history": map[string]any{
				"isComplete": true, "entitiesByKey": map[string]any{"turn:t": map[string]any{"turnId": "t", "status": "inProgress", "items": []any{u, a}}},
				"islands": []any{map[string]any{"entries": []any{map[string]any{"value": "turn:t"}}}},
			}}, "turns": []any{map[string]any{"turnId": "t", "status": "inProgress", "items": []any{u, a}}}}
			broadcast := func(change any) {
				b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Source: "app", Version: version, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": change})})
			}
			broadcast(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state})
			epoch, cursor := p.stream().log.Position()
			steer := map[string]any{"id": "local-steer", "type": "steeringUserMessage", "clientUserMessageId": "client-steer", "targetTurnId": "t", "status": "pending",
				"input": []any{map[string]any{"type": "text", "text": "interruption"}}, "restoreMessage": map[string]any{"createdAt": int64(1794900000123)}}
			broadcast(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{map[string]any{"op": "add", "path": []any{"turns", 0, "items", 1}, "value": steer}}})
			batch, err := p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			if err != nil {
				t.Fatal(err)
			}
			found, refresh := false, false
			for _, event := range batch.Events {
				if event.Kind == "item" && event.ItemID == "local-steer" {
					found = event.Item.Role == "user" && event.Item.Text == "interruption\n"
				}
				refresh = refresh || event.Kind == "snapshot"
			}
			if !found || !refresh {
				t.Fatalf("inserted user message or order reconciliation missing: %+v", batch)
			}
			check := func() {
				t.Helper()
				out, err := p.Transcript(context.Background(), "s", "")
				if err != nil || len(out.Turns) != 1 {
					t.Fatal(out, err)
				}
				items := out.Turns[0].Items
				if len(items) != 3 || items[0].ID != "u" || items[1].ID != "local-steer" || items[2].ID != "a" {
					t.Fatalf("lost or reordered steering bubble: %+v", items)
				}
				if items[1].TimestampMS == nil || *items[1].TimestampMS != 1794900000123 {
					t.Fatal("steering timestamp changed to turn start")
				}
			}
			check()
			broadcast(map[string]any{"type": "patches", "baseRevision": 2, "revision": 3, "patches": []any{
				map[string]any{"op": "replace", "path": []any{"turns", 0, "items", 1, "status"}, "value": "accepted"},
				map[string]any{"op": "add", "path": []any{"turns", 0, "items", 1, "serverUserMessageId"}, "value": "server-steer"},
				map[string]any{"op": "add", "path": []any{"turnHistory", "history", "entitiesByKey", "turn:t", "items", 1}, "value": map[string]any{"id": "server-steer", "type": "userMessage", "clientId": "client-steer", "content": steer["input"]}},
			}})
			check() // The canonical and local representations are one bubble.
		})
	}
}

func TestIPCNewImageMessagesRequestAssetImport(t *testing.T) {
	for _, kind := range []string{"userMessage", "steeringUserMessage"} {
		t.Run(kind, func(t *testing.T) {
			cwd := t.TempDir()
			image := filepath.Join(cwd, "clipboard.png")
			png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
			if err := os.WriteFile(image, png, 0600); err != nil {
				t.Fatal(err)
			}
			p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
			b := newAppIPC(p)
			defer b.cancel()
			p.ipc = b
			b.followed["s"] = &followedThread{owner: "app", changed: make(chan struct{})}
			state := map[string]any{"id": "s", "cwd": cwd, "turns": []any{map[string]any{"turnId": "t", "status": "inProgress", "items": []any{}}}}
			broadcast := func(change any) {
				b.Broadcast(codexipc.Message{Method: "thread-stream-state-changed", Source: "app", Version: 13, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": "s", "change": change})})
			}
			broadcast(map[string]any{"type": "snapshot", "revision": 1, "conversationState": state})
			epoch, cursor := p.stream().log.Position()
			field := "content"
			if kind == "steeringUserMessage" {
				field = "input"
			}
			// Cover both native image inputs and desktop's textual envelope.
			input := []any{map[string]any{"type": "text", "text": "# Files mentioned by the user:\n\n## clipboard.png: " + image + "\nImage attachment: true\n\n## My request:\nlook at this"}, map[string]any{"type": "localImage", "path": image}}
			item := map[string]any{"id": "image-message", "type": kind, field: input}
			broadcast(map[string]any{"type": "patches", "baseRevision": 1, "revision": 2, "patches": []any{map[string]any{"op": "add", "path": []any{"turns", 0, "items", 0}, "value": item}}})
			batch, err := p.stream().log.Probe(context.Background(), "s", epoch, cursor)
			refresh := false
			for _, event := range batch.Events {
				refresh = refresh || event.Kind == "snapshot"
			}
			if err != nil || !refresh {
				t.Fatalf("live attachment never imported: %+v %v", batch, err)
			}
			out, err := p.Transcript(context.Background(), "s", "")
			if err != nil || len(out.Turns) != 1 || len(out.Turns[0].Items) != 1 {
				t.Fatal(out, err)
			}
			got := out.Turns[0].Items[0]
			if got.Role != "user" || len(got.Attachments) != 1 || got.Attachments[0].Kind != "image" || got.Attachments[0].State != "ready" || got.Attachments[0].Source != image {
				t.Fatalf("image list absent or duplicated: %+v", got)
			}
		})
	}
}

func TestIPCTailOrderingAndExplicitMessageIdentity(t *testing.T) {
	item := func(id string) any { return map[string]any{"id": id} }
	for _, tc := range []struct{ history, live, want []any }{
		{[]any{item("u"), item("a")}, []any{item("u"), item("s"), item("a")}, []any{item("u"), item("s"), item("a")}},
		{[]any{item("u"), item("a")}, []any{item("s"), item("a")}, []any{item("u"), item("s"), item("a")}},
		{[]any{item("u"), item("a")}, []any{item("s")}, []any{item("u"), item("a"), item("s")}},
	} {
		if got := mergeIPCItems(tc.history, tc.live); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("partial live tail reordered history: %v, want %v", got, tc.want)
		}
	}
	// Equal text is not identity: two independent sends must remain distinct.
	input := []any{map[string]any{"type": "text", "text": "same instruction"}}
	items := []any{map[string]any{"id": "first", "type": "userMessage", "content": input}, map[string]any{"id": "second", "type": "steeringUserMessage", "input": input}}
	if got := ipcMessageItems(items); len(got) != 2 {
		t.Fatal("independent repeated instruction disappeared")
	}
}
