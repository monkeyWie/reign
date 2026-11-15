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
	connections                                             map[string]*connectionHealth
	connectionDetailTarget                                  string
	suspended                                               bool
	pollCancel                                              context.CancelFunc
	pollGeneration                                          uint64
	presencePolling                                         bool
	presenceGeneration                                      uint64
	devicePresence                                          map[string]Object
	directoryChecking                                       bool
	directoryChecked                                        time.Time
	directoryCancel                                         context.CancelFunc
	timezone                                                *time.Location
	timeline                                                *core.Ref
	pendingSends                                            map[string]string
	outgoing                                                map[string][]*outgoingMessage
	streamKey, streamEpoch                                  string
	streamCursor                                            int64
	connectAttempt                                          *connectAttempt
	workspaces                                              map[string]workspaceCache
	sessionCache                                            map[string]Object
	sessionCacheOrder                                       []string
	cacheLoading, transcriptLoading, localPolling           bool
	catalogueVersion, sessionGeneration                     uint64
	deviceGeneration                                        uint64
	transcriptSyncing, snapshotScheduled                    bool
	snapshots                                               *snapshotWriter
	backgrounded, keepAlive, notificationAsked              bool
	nativeBackgroundState                                   string
	lastTask                                                Object
	pendingNativeSession                                    Object
	nativeTasks                                             map[string]Object
	nativeTaskMonitors                                      map[string]*nativeTaskMonitor
}

func newState(cx *core.Scope, api API) *State {
	ctx, cancel := context.WithCancel(context.Background())
	s := &State{clientName: platformDeviceName(), drafts: map[string]draftState{}, expanded: map[string]bool{}, cx: cx, api: api, ctx: ctx, cancel: cancel, revision: core.NewSignal(cx, 0), width: core.NewSignal(cx, core.Points(1320)), dialog: core.NewSignal(cx, ""), draft: core.NewSignal(cx, ""), deviceWidth: core.NewSignal(cx, core.Points(210)), sessionWidth: core.NewSignal(cx, core.Points(300)), collapsed: map[string]bool{}, images: map[string][]byte{}, loadingImages: map[string]bool{}, previewIndex: core.NewSignal(cx, 0), prompted: map[string]bool{}, retrying: map[string]time.Time{}, timeline: &core.Ref{}}
	s.snapshots = newSnapshotWriter(ctx, api)
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
	// Saved devices are local data; native host startup must not delay browsing.
	s.pollPresence()
	s.observeLifecycle()
	s.refreshTimezone()
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

// Local host discovery and the selected remote catalogue have independent
// lifetimes. A slow local Agent or remote device cannot block cached navigation.
func (s *State) pollLocal() {
	if !s.ready || !s.api.CanHost() || s.localPolling {
		return
	}
	s.localPolling = true
	go func() {
		info, err := s.api.Call(s.ctx, "host.info", nil, "", nil)
		var providers any
		if err == nil {
			providers, err = s.api.Call(s.ctx, "providers", nil, "", nil)
		}
		s.cx.Post(func() {
			s.localPolling = false
			if s.ctx.Err() != nil || s.suspended {
				return
			}
			if info != nil {
				s.host = object(info)
			}
			if providers != nil {
				s.localProviders = visibleProviders(objects(providers))
			}
			s.setConnectionIssue("", "catalogue", err)
			s.changed()
			s.promptHost()
		})
	}()
}

func (s *State) cancelCatalogue() {
	if s.pollCancel != nil {
		s.pollCancel()
		s.pollCancel = nil
	}
	s.pollGeneration++
	s.polling = false
}

func (s *State) poll() {
	if s.suspended || s.ctx.Err() != nil {
		return
	}
	s.pollPresence()
	s.pollLocal()
	if s.polling || s.target == "" || !s.connected() {
		return
	}
	s.polling = true
	s.pollGeneration++
	generation, target := s.pollGeneration, s.target
	ctx, cancel := context.WithCancel(s.ctx)
	s.pollCancel = cancel
	s.changed()
	known := map[string]bool{}
	for _, session := range s.sessions {
		known[str(session, "providerId")] = true
	}
	go s.loadCatalogue(ctx, cancel, generation, target, known)
}

// Presence is local transport state. Never block reconnect detection behind a
// slow transcript/catalogue transfer, and never impose a business-read deadline.
func (s *State) pollPresence() {
	if s.presencePolling {
		return
	}
	s.presencePolling = true
	s.presenceGeneration++
	generation := s.presenceGeneration
	go func() {
		v, err := s.api.Call(s.ctx, "client.list", nil, "", nil)
		s.cx.Post(func() {
			if generation != s.presenceGeneration {
				return
			}
			s.presencePolling = false
			if s.ctx.Err() != nil || s.suspended {
				return
			}
			s.setConnectionIssue("", "presence", err)
			if err == nil {
				s.devices = objects(v)
				s.observeTransports()
				s.openNativeSession()
				s.refreshDevicePresence()
			}
			s.changed()
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
func (s *State) connected() bool { s.read(); return s.connectionReady(s.target) }
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
	s.rememberSession()
	s.rememberWorkspace()
	s.cancelCatalogue()
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.target = id
	s.deviceGeneration++
	s.selected = nil
	s.transcriptSyncing = false
	s.transcript = nil
	s.messageRows = nil
	s.approvals = nil
	s.providers = nil
	s.sessions = nil
	s.cacheLoading = false
	if workspace, ok := s.workspaces[id]; ok {
		s.providers, s.sessions = workspace.providers, workspace.sessions
	}
	s.narrowChat = false
	s.draft.Set("")
	s.uploads = nil
	s.showPhoneSessions()
	s.changed()
	s.loadCachedWorkspace(id)
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
	s.rememberSession()
	s.sessionGeneration++
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.selected = session
	s.transcriptSyncing = false
	s.transcript = s.sessionCache[s.key()]
	s.restoreOutgoing(s.transcript)
	s.rebuildMessages()
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
	s.loadCachedSession()
	s.startSessionWatch()
}
func (s *State) startSessionWatch() {
	if s.selected == nil || s.suspended || !s.connected() {
		return
	}
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.transcriptSyncing = s.transcript == nil
	target, p, id, key := s.target, str(s.selected, "providerId"), str(s.selected, "id"), s.key()
	if s.streamKey != key {
		s.streamKey, s.streamEpoch, s.streamCursor = key, "", 0
	}
	epoch, cursor := s.streamEpoch, s.streamCursor
	ctx, cancel := context.WithCancel(s.ctx)
	s.watchCancel = cancel
	go func() {
		v, err := rpc(ctx, s.api, target, p, id, "models", nil)
		s.cx.Post(func() {
			if ctx.Err() != nil || key != s.key() {
				return
			}
			s.setConnectionIssue(target, "models", err)
			if err == nil {
				s.models = objects(v)
			}
			s.changed()
		})
	}()
	go s.watch(ctx, target, p, id, key, epoch, cursor)
}
func (s *State) setTranscript(t Object) {
	s.transcriptLoading = false
	s.restoreOutgoing(t)
	s.transcript = mergeLatest(s.transcript, t)
	s.reconcileOutgoing(time.Time{})
	s.rebuildMessages()
	if s.model == "" {
		s.model = str(object(t["session"]), "model")
	}
	s.scheduleSnapshot()
	s.changed()
}
func (s *State) watch(ctx context.Context, target, p, id, key, epoch string, cursor int64) {
	snapshot := func() error {
		started := time.Now()
		v, e := rpc(ctx, s.api, target, p, id, "transcript", Object{"fresh": true})
		s.cx.Post(func() {
			if ctx.Err() == nil && s.key() == key {
				s.transcriptSyncing = false
				s.setConnectionIssue(target, "transcript", e)
				if e == nil {
					s.setTranscript(object(v))
					s.reconcileOutgoing(started)
					s.rebuildMessages()
					s.trackNativeTask(target, p, id, object(v))
				} else {
					s.changed()
				}
			}
		})
		return e
	}
	_ = snapshot()
	for ctx.Err() == nil {
		v, e := rpc(ctx, s.api, target, p, id, "watch", Object{"epoch": epoch, "after": cursor})
		if ctx.Err() != nil {
			return
		}
		watchErr := e
		s.cx.Post(func() {
			if ctx.Err() == nil && s.key() == key {
				s.setConnectionIssue(target, "watch", watchErr)
				s.changed()
			}
		})
		if e != nil {
			// Keep the rendered transcript and replay cursor. An offline cache is
			// older than the deltas already on screen, not a replacement for them.
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		b := object(v)
		events := objects(b["events"])
		if flag(b, "reset") || flag(b, "heartbeat") {
			if snapshot() != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
				continue
			}
		} else if len(events) > 0 {
			applied := make(chan bool, 1)
			s.cx.Post(func() {
				if ctx.Err() != nil || s.key() != key {
					applied <- false
					return
				}
				next, reset := applyEvents(s.transcript, events)
				s.transcript = next
				s.trackNativeTask(target, p, id, next)
				s.scheduleSnapshot()
				s.reconcileOutgoing(time.Time{})
				s.rebuildMessages()
				s.changed()
				applied <- reset
			})
			select {
			case reset := <-applied:
				if reset {
					if snapshot() != nil {
						continue
					}
				}
			case <-ctx.Done():
				return
			}
		}
		epoch, cursor = str(b, "epoch"), number(b, "cursor")
		checkpointEpoch, checkpointCursor := epoch, cursor
		s.cx.Post(func() {
			if ctx.Err() == nil && s.key() == key {
				s.streamKey, s.streamEpoch, s.streamCursor = key, checkpointEpoch, checkpointCursor
			}
		})
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
	connected := s.connected()
	s.busy = true
	s.async(func(ctx context.Context) (any, error) {
		v, err := s.api.Call(ctx, "client.cached", nil, target, Object{"method": "transcript", "provider": p, "session": id, "cursor": cursor})
		if err == nil && v != nil {
			return v, nil
		}
		if !connected {
			return nil, fmt.Errorf("更早的消息尚未缓存，请连接设备后加载")
		}
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
		s.rebuildMessages()
		s.scheduleSnapshot()
	})
}
func (s *State) pendingSend() string {
	s.read()
	return s.pendingSends[s.key()]
}
func (s *State) canSend() bool {
	s.read()
	if s.selected == nil || s.busy || !s.connected() || s.pendingSend() != "" {
		return false
	}
	for _, u := range s.uploads {
		if u.Status != "ready" {
			return false
		}
	}
	return strings.TrimSpace(s.draft.Get()) != "" || len(s.uploads) > 0
}
func (s *State) send(method string) {
	if s.selected == nil || s.busy || !s.connected() || s.pendingSend() != "" {
		return
	}
	if method != "interrupt" && !s.canSend() {
		return
	}
	key, target, p, id := s.key(), s.target, str(s.selected, "providerId"), str(s.selected, "id")
	body := strings.TrimSpace(s.draft.Get())
	sent := append([]*Upload{}, s.uploads...)
	ids := []string{}
	for _, u := range sent {
		if u.Status == "ready" {
			ids = append(ids, u.ID)
		}
	}
	extra := Object{"turn": str(activeTurn(s.transcript), "id")}
	if method != "interrupt" {
		extra["text"], extra["attachments"] = body, ids
		if s.model != "" {
			extra["model"] = s.model
		}
	}
	if s.pendingSends == nil {
		s.pendingSends = map[string]string{}
	}
	s.pendingSends[key] = method
	var outgoing *outgoingMessage
	if method != "interrupt" {
		outgoing = s.queueOutgoing(body, sent)
	}
	s.error = ""
	s.changed()
	failure := &sessionOperationError{target: target, device: str(s.device(), "name"), provider: p, session: id, source: str(s.selected, "sourceKind"), method: method}
	go func() {
		_, err := rpc(s.ctx, s.api, target, p, id, method, extra)
		s.cx.Post(func() {
			if s.ctx.Err() != nil {
				return
			}
			delete(s.pendingSends, key)
			confirmed := outgoing != nil && outgoing.Status == "confirmed"
			if err != nil && !confirmed {
				if s.key() == key {
					failure.cause = err
					s.error, s.failure = err.Error(), failure
				}
			}
			if outgoing != nil {
				outgoing.FinishedAt = time.Now()
				if !confirmed {
					outgoing.Status = "accepted"
				}
				if err != nil && !confirmed {
					outgoing.Status, outgoing.Error = "failed", err.Error()
				}
				if s.key() == key {
					s.rebuildMessages()
					s.persistSnapshot()
				} else if cached := s.sessionCache[key]; cached != nil {
					cached["localMessages"] = s.outgoing[key]
					s.writeSnapshot(target, p, id, cached)
				}
				if err == nil && s.key() == key && s.connected() {
					s.refreshAfterSend(key, target, p, id)
				}
			}
			s.changed()
		})
	}()
}
func (s *State) decision(a Object, decision string) {
	target, p, id := s.target, str(s.selected, "providerId"), str(s.selected, "id")
	s.busy = true
	s.async(func(ctx context.Context) (any, error) {
		return rpc(ctx, s.api, target, p, id, "decide", Object{"approval": a["id"], "decision": Object{"decision": decision}})
	}, func(any) { s.approvals = nil })
}
func (s *State) autoReconnect() {
	if s.suspended {
		return
	}
	for _, d := range s.devices {
		id := str(d, "id")
		h := s.connection(id)
		if flag(d, "connected") && h.attempt == nil {
			h.established = true
		}
		if flag(d, "connected") || h.reconnecting || s.deviceOffline(id) {
			continue
		}
		if h.waitingOnline {
			if online, known := s.deviceOnline(d); known && online {
				// A click on an offline device queues one initial attempt, not
				// an infinite retry loop after a failed first connection.
				s.connectDevice(id, h.established, !flag(d, "autoReconnect"))
			}
			continue
		}
		if h.established && flag(d, "autoReconnect") && time.Now().After(s.retrying[id]) {
			s.reconnect(id, true)
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
func pairingCode(v Object) string { b, _ := json.Marshal(v); return "agenthub2:" + base64URL(b) }
