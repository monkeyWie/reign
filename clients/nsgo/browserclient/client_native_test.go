//go:build !js

package browserclient

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/control"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/relay"
	"github.com/monkeyWie/reignhub/internal/remote"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureProvider struct {
	provider.Provider
	sends atomic.Int32
}

func (*fixtureProvider) Status(context.Context) provider.Status {
	return provider.Status{ID: "fixture", State: "ready"}
}
func (*fixtureProvider) Sessions(context.Context, string) (provider.SessionPage, error) {
	return provider.SessionPage{Sessions: []provider.Session{{ID: "fixture-session", Preview: strings.Repeat("跨端消息🙂", 20000)}}}, nil
}
func (p *fixtureProvider) Send(context.Context, string, string) error { p.sends.Add(1); return nil }
func (*fixtureProvider) Transcript(ctx context.Context, _, _ string) (provider.Transcript, error) {
	out := provider.Transcript{Session: provider.Session{ID: "fixture-session"}, StreamEpoch: "history", StreamCursor: 7, NextCursor: "older"}
	turn := provider.Turn{ID: "turn", Status: "completed"}
	for i := range 40 {
		item := provider.Item{ID: fmt.Sprint(i), Role: "assistant", Text: fmt.Sprintf("消息 %d🙂", i)}
		if i == 10 {
			item.Text = strings.Repeat("长回复🙂 mixed text.\n", 2000)
		}
		turn.Items = append(turn.Items, item)
		if err := provider.EmitTranscriptTurn(ctx, out, provider.Turn{ID: turn.ID, Status: turn.Status, Items: []provider.Item{item}}); err != nil {
			return provider.Transcript{}, err
		}
	}
	out.Turns = []provider.Turn{turn}
	return out, nil
}
func TestHostEncryptedInterop(t *testing.T) {
	token := strings.Repeat("r", 32)
	srv := httptest.NewServer(relay.New(token))
	defer srv.Close()
	p := remote.Pairing{Relay: "ws" + strings.TrimPrefix(srv.URL, "http"), Host: relay.ID(), Key: strings.Repeat("ab", 32), RelayToken: token}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor := remote.NewHostStatus(p.Relay)
	fake := &fixtureProvider{}
	go remote.RunHostObserved(ctx, p, remote.Options{RelayOnly: true}, &control.Dispatcher{Providers: map[string]provider.Provider{"fixture": fake}}, monitor)
	deadline := time.Now().Add(5 * time.Second)
	for monitor.Snapshot().State != "connected" {
		if time.Now().After(deadline) {
			t.Fatal("fixture host did not connect")
		}
		time.Sleep(time.Millisecond)
	}
	c, e := Connect(ctx, Pairing{Relay: p.Relay, Host: p.Host, Key: p.Key, Token: token, RelayOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	for range 2 {
		v, e := c.Call(ctx, Object{"method": "sessions", "provider": "fixture"})
		if e != nil {
			t.Fatal(e)
		}
		rows := v.(map[string]any)["sessions"].([]any)
		if len(rows) != 1 || len(rows[0].(map[string]any)["preview"].(string)) < 100000 {
			t.Fatal("large fragmented transcript lost bytes")
		}
		fragments, itemIndex, textParts := 0, 0, 0
		longReply := strings.Repeat("长回复🙂 mixed text.\n", 2000)
		progress := provider.WithTranscriptObserver(ctx, func(fragment provider.Transcript) error {
			if len(fragment.Turns) != 1 || fragment.Turns[0].Items[0].ID != fmt.Sprint(itemIndex) {
				t.Errorf("history reordered at %d", itemIndex)
			}
			item := fragment.Turns[0].Items[0]
			if item.TextOffset != nil {
				textParts++
				if len(item.Text) > provider.TranscriptTextChunkSize {
					t.Error("long reply was not progressive")
				}
				if *item.TextOffset+len(item.Text) == len(longReply) {
					itemIndex++
				}
			} else {
				itemIndex++
			}
			fragments++
			return nil
		})
		history, err := c.Call(progress, Object{"method": "transcript", "provider": "fixture", "session": "fixture-session", "progress": true})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(history)
		var transcript provider.Transcript
		if err := json.Unmarshal(raw, &transcript); err != nil {
			t.Fatal(err)
		}
		if fragments <= 40 || itemIndex != 40 || textParts < 2 || len(transcript.Turns) != 1 || len(transcript.Turns[0].Items) != 40 || transcript.NextCursor != "older" || transcript.StreamCursor != 7 || transcript.Turns[0].Items[10].Text != longReply || transcript.Turns[0].Items[10].TextOffset != nil {
			t.Fatalf("incomplete progressive browser history: fragments=%d transcript=%+v", fragments, transcript)
		}
	}
	if _, e = c.Call(ctx, Object{"method": "send", "provider": "fixture", "session": "fixture-session", "text": "test fixture only"}); e != nil || fake.sends.Load() != 1 {
		t.Fatalf("mutation sent %d times: %v", fake.sends.Load(), e)
	}
	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for c.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("host disconnect was not observed")
		}
		time.Sleep(time.Millisecond)
	}
}
