package hubui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/monkeyWie/reignhub/pkg/composerdraft"
)

type draftWrite struct {
	target, provider, session string
	draft                     composerdraft.Draft
	uploads                   []*Upload
}

func (d draftWrite) key() string { return composerdraft.Key(d.target, d.provider, d.session) }

// One coalescing writer orders edits and consumption. Its immutable snapshots
// contain byte references, never live upload status. Disk work stays off the UI.
type draftWriter struct {
	mu      sync.Mutex
	pending map[string]draftWrite
	wake    chan struct{}
	idle    chan struct{}
	busy    bool
	errors  map[string]error
	blocked map[string]bool
	failed  map[string]draftWrite
}

func newDraftWriter(ctx context.Context, api API, failed func(error)) *draftWriter {
	w := &draftWriter{pending: map[string]draftWrite{}, errors: map[string]error{}, blocked: map[string]bool{}, failed: map[string]draftWrite{}, wake: make(chan struct{}, 1), idle: make(chan struct{})}
	close(w.idle)
	go func() {
		saved := map[string]map[*Upload]composerdraft.File{}
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
			}
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			for {
				w.mu.Lock()
				pending := w.pending
				w.pending = map[string]draftWrite{}
				if len(pending) == 0 {
					if w.busy {
						w.busy = false
						close(w.idle)
					}
					w.mu.Unlock()
					break
				}
				w.mu.Unlock()
				for key, item := range pending {
					next := map[*Upload]composerdraft.File{}
					for i, u := range item.uploads {
						f := item.draft.Files[i]
						if old, ok := saved[key][u]; ok {
							f.Hash = old.Hash
							f.Data = nil
						} else {
							f.Hash = composerdraft.Hash(f.Data)
						}
						item.draft.Files[i] = f
						f.Data = nil
						next[u] = f
					}
					request, cancel := context.WithTimeout(ctx, 10*time.Second)
					_, err := api.Call(request, "client.draft.put", Object{"provider": item.provider, "session": item.session, "draft": item.draft}, item.target, nil)
					cancel()
					w.mu.Lock()
					if err == nil {
						delete(w.errors, key)
						delete(w.failed, key)
					} else {
						w.errors[key] = err
						w.failed[key] = item
					}
					w.mu.Unlock()
					if err == nil {
						saved[key] = next
					} else {
						delete(saved, key)
					}
					if ctx.Err() == nil {
						failed(err)
					}
				}
			}
		}
	}()
	return w
}
func (w *draftWriter) put(item draftWrite) {
	w.mu.Lock()
	if w.blocked[item.target] {
		w.mu.Unlock()
		return
	}
	if !w.busy {
		w.busy = true
		w.idle = make(chan struct{})
	}
	w.pending[item.key()] = item
	delete(w.failed, item.key())
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}
func (w *draftWriter) flush(ctx context.Context) error {
	w.retry()
	w.mu.Lock()
	idle := w.idle
	w.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-idle:
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, err := range w.errors {
		return err
	}
	return nil
}

func (s *State) persistDraft() {
	if s.selected == nil || s.draftWriter == nil || s.draftLoads[s.key()] != "ready" {
		return
	}
	s.rememberDraft()
	s.writeDraft(s.target, str(s.selected, "providerId"), str(s.selected, "id"), s.drafts[s.key()])
}
func (s *State) writeDraft(target, provider, session string, d draftState) {
	item := draftWrite{target: target, provider: provider, session: session, draft: composerdraft.Draft{Text: d.Text, Model: d.Model, ModelBase: d.ModelBase}, uploads: append([]*Upload(nil), d.Uploads...)}
	for _, u := range d.Uploads {
		item.draft.Files = append(item.draft.Files, composerdraft.File{Name: u.Name, MIME: u.MIME, Size: len(u.Data), Data: u.Data})
	}
	s.draftWriter.put(item)
}
func (s *State) draftReady() bool { return s.draftWriter == nil || s.draftLoads[s.key()] == "ready" }
func (s *State) loadDraft() {
	if s.draftWriter == nil || s.selected == nil {
		return
	}
	key, target, p, id := s.key(), s.target, str(s.selected, "providerId"), str(s.selected, "id")
	if s.draftLoads[key] == "ready" {
		s.resumeDraftUploads()
		return
	}
	if s.draftLoads[key] == "loading" {
		return
	}
	s.draftLoads[key] = "loading"
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		v, err := s.api.Call(ctx, "client.draft.get", Object{"provider": p, "session": id}, target, nil)
		var d composerdraft.Draft
		if err == nil {
			var b []byte
			b, err = json.Marshal(v)
			if err == nil {
				err = json.Unmarshal(b, &d)
			}
		}
		s.cx.Post(func() {
			if s.ctx.Err() != nil || s.draftLoads[key] != "loading" {
				return
			}
			if err != nil {
				s.draftLoads[key] = "failed"
				s.error = "草稿恢复失败：" + err.Error()
				s.changed()
				return
			}
			draft := draftState{Text: d.Text, Model: d.Model, ModelBase: d.ModelBase}
			for _, f := range d.Files {
				draft.Uploads = append(draft.Uploads, &Upload{Name: f.Name, MIME: f.MIME, Data: f.Data, Status: "restored", Target: target, Provider: p, Session: id})
			}
			s.drafts[key] = draft
			s.draftLoads[key] = "ready"
			if s.key() == key {
				s.draft.Set(draft.Text)
				s.uploads = draft.Uploads
				s.restoreDraftModel(draft)
				s.resumeDraftUploads()
			}
			s.changed()
		})
	}()
}
func (s *State) resumeDraftUploads() {
	if !s.connected() {
		return
	}
	for _, u := range s.uploads {
		if u.Status == "restored" {
			s.upload(u)
		}
	}
}
func (s *State) loadLastSession() {
	var last Object
	if raw, ok := s.cx.Storage().GetItem("session-last"); ok && json.Unmarshal([]byte(raw), &last) == nil {
		s.restoreSession = last
	}
}
func (s *State) saveLastSession() {
	if s.selected == nil {
		return
	}
	last := Object{"deviceId": s.target, "providerId": str(s.selected, "providerId"), "id": str(s.selected, "id")}
	raw, err := json.Marshal(last)
	if err == nil {
		err = s.cx.Storage().SetItem("session-last", string(raw))
	}
	if err != nil {
		s.error = fmt.Sprintf("无法保存上次访问的会话：%v", err)
	}
}
func (s *State) restoreLastSession() {
	if s.restoreSession == nil || s.selected != nil {
		return
	}
	for _, ss := range s.sessions {
		if sessionIdentity(ss) == sessionIdentity(s.restoreSession) && !s.removedSessions[sessionIdentity(ss)] {
			s.restoreSession = nil
			s.selectSession(ss)
			return
		}
	}
}

func (w *draftWriter) block(target string, value bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.blocked[target] = value
}

func (s *State) discardSessionDraft(target, provider, session string) {
	key := target + "|" + provider + "|" + session
	d := s.drafts[key]
	if s.key() == key {
		d.Uploads = s.uploads
		s.uploads = nil
		s.draft.Set("")
	}
	for _, u := range d.Uploads {
		u.cancelled.Store(true)
		if u.cancel != nil {
			u.cancel()
		}
	}
	delete(s.drafts, key)
	delete(s.draftLoads, key)
	if s.draftWriter != nil {
		s.writeDraft(target, provider, session, draftState{})
	}
	s.clearLastSession(target, provider, session)
}
func (s *State) clearLastSession(target, provider, session string) {
	var last Object
	raw, ok := s.cx.Storage().GetItem("session-last")
	if ok && json.Unmarshal([]byte(raw), &last) == nil && str(last, "deviceId") == target && (session == "" || str(last, "id") == session && str(last, "providerId") == provider) {
		_ = s.cx.Storage().SetItem("session-last", "null")
		s.restoreSession = nil
	}
}

func (w *draftWriter) hasFailures() bool { w.mu.Lock(); defer w.mu.Unlock(); return len(w.errors) > 0 }
func (w *draftWriter) retry() {
	w.mu.Lock()
	queued := false
	for key, item := range w.failed {
		if _, newer := w.pending[key]; newer || w.blocked[item.target] {
			continue
		}
		// Retry storage only; file bytes are immutable and this never resends a message.
		item.draft.Files = append([]composerdraft.File(nil), item.draft.Files...)
		for i, u := range item.uploads {
			item.draft.Files[i].Data = u.Data
		}
		w.pending[key] = item
		queued = true
	}
	if queued && !w.busy {
		w.busy = true
		w.idle = make(chan struct{})
	}
	w.mu.Unlock()
	if queued {
		select {
		case w.wake <- struct{}{}:
		default:
		}
	}
}
