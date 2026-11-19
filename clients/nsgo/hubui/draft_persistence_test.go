package hubui

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/pkg/composerdraft"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

type draftMemory struct {
	mu     sync.Mutex
	values map[string]json.RawMessage
}

func (s *draftMemory) Get(k string) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v any
	if b := s.values[k]; b != nil {
		if e := json.Unmarshal(b, &v); e != nil {
			return nil, e
		}
	}
	return v, nil
}
func (s *draftMemory) Put(k string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, e := json.Marshal(v)
	if e == nil {
		s.values[k] = b
	}
	return e
}
func (s *draftMemory) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, k)
	return nil
}
func (s *draftMemory) Keys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.values {
		keys = append(keys, k)
	}
	return keys, nil
}

type persistentDemo struct {
	*demoAPI
	store *draftMemory
}

func (a *persistentDemo) Call(ctx context.Context, m string, p Object, target string, r Object) (any, error) {
	if m == "client.draft.get" || m == "client.draft.put" {
		return composerdraft.Call(ctx, a.store, m, target, p)
	}
	return a.demoAPI.Call(ctx, m, p, target, r)
}
func TestComposerDraftRestartRestoresTextFilesAndLastSession(t *testing.T) {
	api := &persistentDemo{newDemoAPI(), &draftMemory{values: map[string]json.RawMessage{}}}
	var s *State
	var restart func()
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		generation := core.NewSignal(cx, 0)
		restart = func() { generation.Update(func(n int) int { return n + 1 }) }
		return core.Dynamic(func(cx *core.Scope) core.Node {
			generation.Get()
			s = newState(cx, api)
			s.applyColors()
			s.initialize()
			return s.workspaceView(cx)
		})
	}, nsgotest.Size(1100, 900), nsgotest.Environment(core.Environment{Idiom: core.IdiomDesktop, OS: core.OSMacOS, Locale: "zh-CN"}))
	app.WaitFor(sessionSelector("demo-device", "codex-app", "demo-chat"), 5*time.Second)
	app.Press(sessionSelector("demo-device", "codex-app", "demo-chat"))
	app.WaitUntil(time.Second, func() bool { return !app.Find("id:chat-input").Props.Disabled })
	app.Type("id:chat-input", "saved draft")
	app.Post(func() {
		s.uploads = []*Upload{{Name: "photo.png", MIME: "image/png", Data: []byte("image bytes"), Status: "failed"}, {Name: "report.txt", MIME: "text/plain", Data: []byte("report bytes"), Status: "failed"}}
		s.persistDraft()
		s.changed()
	})
	app.Settle()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := s.draftWriter.flush(ctx); e != nil {
		t.Fatal(e)
	}
	old := s
	app.Post(restart)
	app.WaitUntil(5*time.Second, func() bool { return s != old })
	app.WaitFor("id:chat-input", 5*time.Second)
	app.WaitUntil(5*time.Second, func() bool { return app.Find("id:chat-input").Text == "saved draft" })
	if len(s.uploads) != 2 || string(s.uploads[0].Data) != "image bytes" || string(s.uploads[1].Data) != "report bytes" {
		t.Fatal("attachments did not survive app state replacement")
	}
	// A send consumes this draft; completion must not erase a newer edit.
	app.Post(func() { s.uploads = nil; s.queueOutgoing("saved draft", nil); s.changeDraft("next draft") })
	app.Settle()
	if e := s.draftWriter.flush(ctx); e != nil {
		t.Fatal(e)
	}
	old = s
	app.Post(restart)
	app.WaitUntil(5*time.Second, func() bool { return s != old })
	app.WaitUntil(5*time.Second, func() bool { return app.Exists("id:chat-input") && app.Find("id:chat-input").Text == "next draft" })
	if len(s.uploads) != 0 {
		t.Fatal("consumed attachments resurrected")
	}
}

type delayedDraftAPI struct {
	*persistentDemo
	entered, release chan struct{}
	once             sync.Once
	fail             atomic.Bool
}

func (a *delayedDraftAPI) Call(ctx context.Context, m string, p Object, target string, r Object) (any, error) {
	if m == "client.draft.put" {
		a.once.Do(func() {
			close(a.entered)
			select {
			case <-a.release:
			case <-ctx.Done():
			}
		})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if a.fail.Load() {
			return nil, errors.New("fixture storage unavailable")
		}
	}
	return a.persistentDemo.Call(ctx, m, p, target, r)
}
func TestDraftWriterDelayedSaveConsumptionAndFailureRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &delayedDraftAPI{persistentDemo: &persistentDemo{newDemoAPI(), &draftMemory{values: map[string]json.RawMessage{}}}, entered: make(chan struct{}), release: make(chan struct{})}
	w := newDraftWriter(ctx, a, func(error) {})
	u := &Upload{Name: "file.txt", MIME: "text/plain", Data: []byte("bytes")}
	item := draftWrite{target: "device", provider: "codex", session: "session", draft: composerdraft.Draft{Text: "old", Files: []composerdraft.File{{Name: u.Name, MIME: u.MIME, Size: len(u.Data), Data: u.Data}}}, uploads: []*Upload{u}}
	w.put(item)
	select {
	case <-a.entered:
	case <-time.After(time.Second):
		t.Fatal("write never began")
	}
	consumed := item
	consumed.draft = composerdraft.Draft{}
	consumed.uploads = nil
	w.put(consumed)
	fresh := consumed
	fresh.draft.Text = "new after send"
	w.put(fresh)
	close(a.release)
	flush, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if err := w.flush(flush); err != nil {
		t.Fatal(err)
	}
	p := Object{"provider": "codex", "session": "session"}
	v, err := a.Call(ctx, "client.draft.get", p, "device", nil)
	if err != nil || v.(composerdraft.Draft).Text != "new after send" || len(v.(composerdraft.Draft).Files) != 0 {
		t.Fatal("late save resurrected consumed draft", v, err)
	}
	a.fail.Store(true)
	fresh.draft.Text = "kept through failure"
	w.put(fresh)
	if err = w.flush(flush); err == nil {
		t.Fatal("storage failure not reported")
	}
	a.fail.Store(false)
	if err = w.flush(flush); err != nil {
		t.Fatal("retry failed", err)
	}
	v, err = a.Call(ctx, "client.draft.get", p, "device", nil)
	if err != nil || v.(composerdraft.Draft).Text != "kept through failure" {
		t.Fatal("retry lost content", v, err)
	}
}
