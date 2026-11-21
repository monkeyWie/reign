package codex

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func TestSmallOpeningPageContinuesThroughOlderTurns(t *testing.T) {
	for _, source := range []string{"app-server", "ipc"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("CODEX_HOME", t.TempDir())
			p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
			turns := make([]any, 30)
			for i := range turns {
				turns[i] = map[string]any{"id": fmt.Sprint(i), "status": "completed", "items": []any{map[string]any{"id": "reply", "type": "agentMessage", "text": fmt.Sprint(i)}}}
			}
			if source == "ipc" {
				b := newAppIPC(p)
				t.Cleanup(b.cancel)
				b.home = t.TempDir()
				b.queueFiles = nil
				p.ipc = b
				b.followed["s"] = &followedThread{owner: "owner", state: rawJSON(map[string]any{"id": "s", "turns": turns})}
			} else {
				backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
					switch m.Method {
					case "thread/read":
						return map[string]any{"thread": map[string]any{"id": "s"}}, nil
					case "thread/turns/list":
						args := decodeObject(m.Params)
						end := len(turns)
						if cursor := stringValue(args["cursor"]); cursor != "" {
							var err error
							end, err = strconv.Atoi(cursor)
							if err != nil {
								t.Error(err)
							}
						}
						begin := max(0, end-int(args["limit"].(float64)))
						page := map[string]any{}
						var rows []any
						for i := end - 1; i >= begin; i-- {
							rows = append(rows, turns[i])
						}
						page["data"] = rows
						if begin > 0 {
							page["nextCursor"] = fmt.Sprint(begin)
						}
						return page, nil
					default:
						t.Errorf("unexpected method %s", m.Method)
						return nil, nil
					}
				})
			}
			ctx := context.Background()
			head, err := p.Transcript(provider.WithRecentTurns(ctx, 4), "s", "")
			if err != nil || len(head.Turns) != 4 || head.NextCursor == "" {
				t.Fatalf("opening: %d turns, %v", len(head.Turns), err)
			}
			prev, err := p.Transcript(ctx, "s", head.NextCursor)
			if err != nil || prev.NextCursor != "" || len(prev.Turns) != 26 {
				t.Fatalf("continuation: %d turns, %v", len(prev.Turns), err)
			}
			for i, turn := range append(prev.Turns, head.Turns...) {
				if turn.ID != fmt.Sprint(i) || len(turn.Items) != 1 || turn.Items[0].Text != fmt.Sprint(i) {
					t.Fatalf("missing or reordered turn %d", i)
				}
			}
		})
	}
}

func TestHugeTurnItemPages(t *testing.T) {
	for _, source := range []string{"ipc", "app-server"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("CODEX_HOME", t.TempDir())
			p := New(config.Config{DataDir: t.TempDir()}, "", events.New())
			items := make([]any, 500)
			for i := range items {
				items[i] = map[string]any{"id": fmt.Sprint(i), "type": "agentMessage", "text": fmt.Sprintf("fixture message %d", i)}
			}
			turn := map[string]any{"id": "large", "status": "completed", "items": items}
			if source == "ipc" {
				b := newAppIPC(p)
				t.Cleanup(b.cancel)
				b.home = t.TempDir()
				p.ipc = b
				b.followed["s"] = &followedThread{owner: "owner", state: rawJSON(map[string]any{"id": "s", "turns": []any{turn}})}
			} else {
				backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
					switch m.Method {
					case "thread/read":
						return map[string]any{"thread": map[string]any{"id": "s"}}, nil
					case "thread/turns/list":
						return map[string]any{"data": []any{turn}}, nil
					default:
						t.Errorf("unexpected method %s", m.Method)
						return nil, nil
					}
				})
			}
			full, err := p.Transcript(context.Background(), "s", "")
			if err != nil || len(full.Turns) != 1 || len(full.Turns[0].Items) != 500 {
				t.Fatalf("baseline: %v", err)
			}
			cursor := ""
			for page := range 10 {
				count := 0
				ctx := provider.WithTranscriptObserver(provider.WithHistoryItems(context.Background(), 50), func(f provider.Transcript) error {
					for _, turn := range f.Turns {
						count += len(turn.Items)
					}
					return nil
				})
				got, err := p.Transcript(ctx, "s", cursor)
				if err != nil || len(got.Turns) != 1 || len(got.Turns[0].Items) != 50 || count != 50 {
					t.Fatalf("page %d: %v, %d streamed", page, err, count)
				}
				if page == 0 {
					t.Logf("snapshot JSON: full=%d bytes, first page=%d bytes", len(rawJSON(full)), len(rawJSON(got)))
				}
				for i, v := range got.Turns[0].Items {
					if v.ID != fmt.Sprint(450-50*page+i) {
						t.Fatalf("page %d expected item %d, got %s", page, i, v.ID)
					}
				}
				if (got.NextCursor == "") != (page == 9) || (got.PartialTurn == "large") != (page < 9) {
					t.Fatal("incorrect page frontier", page)
				}
				cursor = got.NextCursor
			}
		})
	}
}
