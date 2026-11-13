package hubui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

type draftState struct {
	Text, Model string
	Uploads     []*Upload
}

type State struct {
	phoneNav                                                *base.Nav[string]
	clientName                                              string
	drafts                                                  map[string]draftState
	expanded                                                map[string]bool
	createDirectory, directoryInfo                          string
	cx                                                      *core.Scope
	api                                                     API
	ctx                                                     context.Context
	cancel                                                  context.CancelFunc
	revision                                                *core.Signal[int]
	width                                                   *core.Signal[core.Points]
	dialog                                                  *core.Signal[string]
	devices, localProviders, providers, sessions, approvals []Object
	host, selected, transcript, scanned                     Object
	accountLogin, accountStatus                             Object
	accountDevices                                          []Object
	target, error, notice, model                            string
	failure                                                 *sessionOperationError
	copiedValue                                             string
	copyVersion                                             uint64
	ready, busy, polling, narrowChat, dark                  bool
	draft                                                   *core.Signal[string]
	deviceWidth, sessionWidth                               *core.Signal[core.Points]
	collapsed                                               map[string]bool
	models                                                  []Object
	messageRows                                             []Message
	uploads                                                 []*Upload
	images                                                  map[string][]byte
	loadingImages                                           map[string]bool
	preview                                                 []Object
	previewIndex                                            *core.Signal[int]
	watchCancel                                             context.CancelFunc
	prompted                                                map[string]bool
	retrying                                                map[string]time.Time
	timeline                                                *core.Ref
}

func newState(cx *core.Scope, api API) *State {
	ctx, cancel := context.WithCancel(context.Background())
	s := &State{clientName: platformDeviceName(), drafts: map[string]draftState{}, expanded: map[string]bool{}, cx: cx, api: api, ctx: ctx, cancel: cancel, revision: core.NewSignal(cx, 0), width: core.NewSignal(cx, core.Points(1320)), dialog: core.NewSignal(cx, ""), draft: core.NewSignal(cx, ""), deviceWidth: core.NewSignal(cx, core.Points(210)), sessionWidth: core.NewSignal(cx, core.Points(300)), collapsed: map[string]bool{}, images: map[string][]byte{}, loadingImages: map[string]bool{}, previewIndex: core.NewSignal(cx, 0), prompted: map[string]bool{}, retrying: map[string]time.Time{}, timeline: &core.Ref{}}
	cx.Cleanup(func() {
		cancel()
		if s.watchCancel != nil {
			s.watchCancel()
		}
	})
	cx.OnQuit(func(context.Context) { cancel(); go api.Close() })
	return s
}
func (s *State) read()    { s.revision.Get() }
func (s *State) changed() { s.revision.Update(func(n int) int { return n + 1 }) }
func (s *State) async(work func(context.Context) (any, error), done func(any)) {
	go func() {
		v, err := work(s.ctx)
		s.cx.Post(func() {
			if s.ctx.Err() != nil {
				return
			}
			s.busy = false
			if err != nil {
				s.error = err.Error()
				s.failure = nil
				_ = errors.As(err, &s.failure)
			} else if done != nil {
				done(v)
			}
			s.changed()
		})
	}()
}
func (s *State) call(method string, p Object, done func(any)) {
	s.busy = true
	s.error = ""
	s.changed()
	s.async(func(ctx context.Context) (any, error) { return s.api.Call(ctx, method, p, "", nil) }, func(v any) {
		if done != nil {
			done(v)
		}
		s.poll()
	})
}
func (s *State) initialize() {
	resolveDeviceName(s.cx, func(name string) { s.clientName = name; s.changed() })
	s.async(func(ctx context.Context) (any, error) {
		if s.api.CanHost() {
			if _, e := s.api.Call(ctx, "host.bootstrap", nil, "", nil); e != nil {
				return nil, e
			}
			v, e := s.api.Call(ctx, "host.info", nil, "", nil)
			if e != nil {
				return nil, e
			}
			info := object(v)
			params := Object{"serviceUrl": str(info, "serviceUrl"), "name": str(info, "name")}
			configure := false
			if str(info, "serviceUrl") == "" {
				params["serviceUrl"] = serviceOrigin()
				configure = true
			}
			if name := str(info, "name"); name == "" || name == "RAS" || name == "Agent Hub" {
				params["name"] = platformDeviceName()
				configure = true
			}
			if configure {
				if _, e = s.api.Call(ctx, "host.configure", params, "", nil); e != nil {
					return nil, e
				}
			}
		}
		return nil, nil
	}, func(any) { s.ready = true; s.poll() })
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-tick.C:
				s.cx.Post(s.poll)
			}
		}
	}()
}

type pollResult struct {
	devices, local, providers, sessions []Object
	host                                Object
	target                              string
}

func (s *State) poll() {
	if !s.ready || s.polling || s.ctx.Err() != nil {
		return
	}
	s.polling = true
	target := s.target
	go func() {
		r := pollResult{target: target}
		v, err := s.api.Call(s.ctx, "client.list", nil, "", nil)
		r.devices = objects(v)
		if s.api.CanHost() && err == nil {
			v, err = s.api.Call(s.ctx, "host.info", nil, "", nil)
			r.host = object(v)
			if err == nil {
				v, err = s.api.Call(s.ctx, "providers", nil, "", nil)
				r.local = visibleProviders(objects(v))
			}
		}
		if target != "" && err == nil {
			v, err = rpc(s.ctx, s.api, target, "", "", "providers", nil)
			r.providers = visibleProviders(objects(v))
			if err == nil {
				for _, p := range r.providers {
					if !providerReady(p) {
						continue
					}
					var list any
					list, err = s.api.Call(s.ctx, "client.sync", Object{"provider": p["id"]}, target, nil)
					if err != nil {
						break
					}
					r.sessions = append(r.sessions, objects(list)...)
				}
			}
		}
		s.cx.Post(func() {
			s.polling = false
			if s.ctx.Err() != nil {
				return
			}
			// Presence must still refresh if a remote catalogue request fails.
			if r.devices != nil {
				s.devices = r.devices
			}
			if r.host != nil {
				s.host = r.host
			}
			if r.local != nil {
				s.localProviders = r.local
			}
			if err != nil {
				s.error = err.Error()
			} else if s.target == r.target {
				s.providers = r.providers
				s.sessions = r.sessions
			}
			s.changed()
			s.promptHost()
			s.autoReconnect()
		})
	}()
}
func visibleProviders(in []Object) []Object {
	out := []Object{}
	for _, p := range in {
		if str(p, "id") != "codex-vscode" {
			out = append(out, p)
		}
	}
	return out
}
func providerReady(p Object) bool {
	return str(p, "state") == "ready" || str(p, "state") == "connected"
}
func (s *State) device() Object {
	for _, d := range s.devices {
		if str(d, "id") == s.target {
			return d
		}
	}
	return Object{}
}
func (s *State) connected() bool { s.read(); return flag(s.device(), "connected") }
func (s *State) key() string {
	return s.target + "|" + str(s.selected, "providerId") + "|" + str(s.selected, "id")
}
func (s *State) rememberDraft() {
	if s.selected != nil {
		s.drafts[s.key()] = draftState{s.draft.GetUntracked(), s.model, s.uploads}
	}
}
func (s *State) selectDevice(id string) {
	if s.target == id {
		s.showPhoneSessions()
		return
	}
	s.rememberDraft()
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.target = id
	s.selected = nil
	s.transcript = nil
	s.messageRows = nil
	s.approvals = nil
	s.providers = nil
	s.sessions = nil
	s.narrowChat = false
	s.draft.Set("")
	s.uploads = nil
	s.showPhoneSessions()
	s.changed()
	s.poll()
}
func (s *State) selectSession(session Object) {
	if s.key() == s.target+"|"+str(session, "providerId")+"|"+str(session, "id") {
		s.narrowChat = true
		s.showPhoneChat()
		s.changed()
		return
	}
	s.rememberDraft()
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.selected = session
	s.transcript = nil
	s.messageRows = nil
	s.approvals = nil
	s.models = nil
	s.model = str(session, "model")
	s.narrowChat = true
	d := s.drafts[s.key()]
	s.draft.Set(d.Text)
	s.uploads = d.Uploads
	if d.Model != "" {
		s.model = d.Model
	}
	s.timeline = &core.Ref{}
	s.showPhoneChat()
	s.changed()
	target, p, id, key := s.target, str(session, "providerId"), str(session, "id"), s.key()
	ctx, cancel := context.WithCancel(s.ctx)
	s.watchCancel = cancel
	s.async(func(ctx context.Context) (any, error) { return rpc(ctx, s.api, target, p, id, "models", nil) }, func(v any) {
		if key == s.key() {
			s.models = objects(v)
		}
	})
	go s.watch(ctx, target, p, id, key)
}
func (s *State) setTranscript(t Object) {
	s.transcript = mergeLatest(s.transcript, t)
	s.messageRows = messages(s.transcript)
	if s.model == "" {
		s.model = str(object(t["session"]), "model")
	}
	s.changed()
}
func (s *State) watch(ctx context.Context, target, p, id, key string) {
	epoch := ""
	var cursor int64
	snapshot := func() error {
		v, e := rpc(ctx, s.api, target, p, id, "transcript", nil)
		if e == nil {
			s.cx.Post(func() {
				if ctx.Err() == nil && s.key() == key {
					s.setTranscript(object(v))
				}
			})
		}
		return e
	}
	_ = snapshot()
	for ctx.Err() == nil {
		v, e := rpc(ctx, s.api, target, p, id, "watch", Object{"epoch": epoch, "after": cursor})
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			epoch = ""
			_ = snapshot()
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		b := object(v)
		epoch = str(b, "epoch")
		cursor = number(b, "cursor")
		events := objects(b["events"])
		if flag(b, "reset") || flag(b, "heartbeat") {
			_ = snapshot()
		} else if len(events) > 0 {
			applied := make(chan bool, 1)
			s.cx.Post(func() {
				if ctx.Err() != nil || s.key() != key {
					applied <- false
					return
				}
				next, reset := applyEvents(s.transcript, events)
				s.transcript = next
				s.messageRows = messages(next)
				s.changed()
				applied <- reset
			})
			select {
			case reset := <-applied:
				if reset {
					_ = snapshot()
				}
			case <-ctx.Done():
				return
			}
		}
		v, e = rpc(ctx, s.api, target, p, id, "approvals", nil)
		if e == nil {
			s.cx.Post(func() {
				if ctx.Err() == nil && s.key() == key {
					s.approvals = objects(v)
					s.changed()
				}
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(32 * time.Millisecond):
		}
	}
}
func (s *State) older() {
	cursor := str(s.transcript, "nextCursor")
	if cursor == "" || s.busy {
		return
	}
	key, target, p, id := s.key(), s.target, str(s.selected, "providerId"), str(s.selected, "id")
	s.busy = true
	s.async(func(ctx context.Context) (any, error) {
		return rpc(ctx, s.api, target, p, id, "transcript", Object{"cursor": cursor})
	}, func(v any) {
		if key != s.key() || str(s.transcript, "nextCursor") != cursor {
			return
		}
		page := object(v)
		seen := map[string]bool{}
		current := objects(s.transcript["turns"])
		for _, t := range current {
			seen[str(t, "id")] = true
		}
		older := []Object{}
		for _, t := range objects(page["turns"]) {
			if !seen[str(t, "id")] {
				older = append(older, t)
			}
		}
		s.transcript["turns"] = append(older, current...)
		s.transcript["nextCursor"] = page["nextCursor"]
		s.messageRows = messages(s.transcript)
	})
}
func (s *State) send(method string) {
	if s.busy || !s.connected() || s.selected == nil {
		return
	}
	key, target, p, id := s.key(), s.target, str(s.selected, "providerId"), str(s.selected, "id")
	body := strings.TrimSpace(s.draft.Get())
	ids := []string{}
	for _, u := range s.uploads {
		if u.Status != "ready" && method != "interrupt" {
			return
		}
		if u.Status == "ready" {
			ids = append(ids, u.ID)
		}
	}
	if method != "interrupt" && body == "" && len(ids) == 0 {
		return
	}
	extra := Object{"turn": str(activeTurn(s.transcript), "id")}
	if method != "interrupt" {
		extra["text"] = body
		extra["attachments"] = ids
		if s.model != "" {
			extra["model"] = s.model
		}
	}
	s.busy = true
	s.error = ""
	s.changed()
	failure := &sessionOperationError{target: target, device: str(s.device(), "name"), provider: p, session: id, source: str(s.selected, "sourceKind"), method: method}
	s.async(func(ctx context.Context) (any, error) {
		v, err := rpc(ctx, s.api, target, p, id, method, extra)
		if err != nil {
			failure.cause = err
			return nil, failure
		}
		return v, nil
	}, func(any) {
		if s.key() != key && method != "interrupt" {
			if d := s.drafts[key]; strings.TrimSpace(d.Text) == body {
				d.Text = ""
				d.Uploads = nil
				s.drafts[key] = d
			}
		}
		if s.key() == key && method != "interrupt" {
			if strings.TrimSpace(s.draft.Get()) == body {
				s.draft.Set("")
			}
			s.uploads = nil
			s.model = str(object(s.transcript["session"]), "model")
		}
	})
}
func (s *State) decision(a Object, decision string) {
	target, p, id := s.target, str(s.selected, "providerId"), str(s.selected, "id")
	s.busy = true
	s.async(func(ctx context.Context) (any, error) {
		return rpc(ctx, s.api, target, p, id, "decide", Object{"approval": a["id"], "decision": Object{"decision": decision}})
	}, func(any) { s.approvals = nil })
}
func (s *State) autoReconnect() {
	for _, d := range s.devices {
		if !flag(d, "connected") && flag(d, "autoReconnect") && time.Now().After(s.retrying[str(d, "id")]) {
			id := str(d, "id")
			s.retrying[id] = time.Now().Add(15 * time.Second)
			go func() { _, _ = s.api.Call(s.ctx, "client.reconnect", Object{"id": id}, "", nil) }()
		}
	}
}
func (s *State) promptHost() {
	if s.dialog.GetUntracked() != "" {
		return
	}
	for _, p := range objects(s.host["pending"]) {
		id := str(p, "id")
		if !s.prompted[id] {
			s.prompted[id] = true
			s.dialog.Set("authorize:" + id)
			return
		}
	}
}
func (s *State) copy(value string) {
	if value == "" {
		return
	}
	s.cx.SetClipboard(value)
	s.copiedValue = value
	s.copyVersion++
	version := s.copyVersion
	s.changed()
	time.AfterFunc(2*time.Second, func() {
		s.cx.Post(func() {
			if s.ctx.Err() != nil || s.copyVersion != version {
				return
			}
			s.copiedValue = ""
			s.changed()
		})
	})
}
func (s *State) toggleTheme() {
	s.dark = !s.dark
	if s.dark {
		s.cx.SetAppearance(core.AppearanceDark)
	} else {
		s.cx.SetAppearance(core.AppearanceLight)
	}
	_ = s.cx.Storage().SetItem("appearance", fmt.Sprint(s.dark))
	s.changed()
}
func (s *State) connect(p Object) {
	s.busy = true
	s.notice = "正在连接，首次访问请在被控电脑确认…"
	s.changed()
	s.async(func(ctx context.Context) (any, error) { return s.api.Call(ctx, "client.connect", p, "", nil) }, func(v any) {
		s.devices = objects(v)
		s.notice = "已连接，正在同步会话"
		for _, d := range s.devices {
			if str(d, "connectionId") == strings.ReplaceAll(str(p, "code"), " ", "") {
				s.selectDevice(str(d, "id"))
				break
			}
		}
		s.poll()
	})
}

func pairingCode(v Object) string { b, _ := json.Marshal(v); return "agenthub2:" + base64URL(b) }
