package codex

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestIPCValidCursorRestoresAndRetainsFollower(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "missing"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) {
			p := New(config.Config{}, "", events.New())
			backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
				if m.Method == "thread/queue/list" {
					return nil, &rpc.Error{Code: -32601, Message: "Unsupported method"}
				}
				t.Errorf("subscription reached private executor: %s", m.Method)
				return nil, nil
			})
			var f *ipcFixture
			f = attachIPCFixture(t, p, func(m codexipc.Message) (codexipc.Message, bool) {
				if m.Method == "thread-stream-following-changed" {
					var params struct{ Following bool }
					_ = json.Unmarshal(m.Params, &params)
					if params.Following {
						go f.snapshot("external", 1, "latest reply")
					}
				}
				return codexipc.Message{}, false
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if directory {
				owner, err := f.b.followFor(ctx, "external", true)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.b.waitState(ctx, "external", owner); err != nil {
					t.Fatal(err)
				}
			}
			// A persisted transcript or sidebar follower can already supply a
			// valid replay cursor before this conversation is explicitly opened.
			epoch, cursor := p.stream().log.Position()
			batch, err := p.Probe(ctx, "external", epoch, cursor)
			if err != nil || batch.Reset {
				t.Fatalf("valid replay boundary: %+v %v", batch, err)
			}
			out, ok, err := p.ipcTranscript(ctx, "external", "")
			if err != nil || !ok || len(out.Turns) != 1 || out.Turns[0].Items[0].Text != "latest reply" {
				t.Fatalf("resumed watch did not attach to owner: %+v %t %v", out, ok, err)
			}
			// A subsequent directory refresh must not unsubscribe the selected
			// conversation when its writer lock is released between turns.
			f.b.releaseDirectoryFollower(ctx, "external")
			f.snapshot("external", 2, "latest reply continues")
			for {
				batch, err = p.Watch(ctx, "external", batch.Epoch, batch.Cursor)
				if err != nil {
					t.Fatalf("live reply stopped after directory cleanup: %v", err)
				}
				for _, event := range batch.Events {
					if event.Kind == "text" && event.Delta == " continues" {
						return
					}
				}
			}
		})
	}
}
