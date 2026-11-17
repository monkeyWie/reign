package hubui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
	"time"
)

type workspaceCache struct{ providers, sessions []Object }

func (s *State) rememberWorkspace() {
	if s.target == "" {
		return
	}
	if s.workspaces == nil {
		s.workspaces = map[string]workspaceCache{}
	}
	s.workspaces[s.target] = workspaceCache{s.providers, s.sessions}
}

func (s *State) rememberSession() {
	s.persistSnapshot()
	if s.selected == nil || s.transcript == nil {
		return
	}
	if s.sessionCache == nil {
		s.sessionCache = map[string]Object{}
	}
	key := s.key()
	if _, exists := s.sessionCache[key]; !exists {
		s.sessionCacheOrder = append(s.sessionCacheOrder, key)
	}
	s.sessionCache[key] = s.snapshotProjection()
	// Disk remains the source for other sessions; bound the in-memory history.
	for len(s.sessionCacheOrder) > 6 {
		delete(s.sessionCache, s.sessionCacheOrder[0])
		s.sessionCacheOrder = s.sessionCacheOrder[1:]
	}
}

func (s *State) loadCachedWorkspace(target string) {
	if target == "" {
		return
	}
	s.cacheLoading = true
	version, generation := s.catalogueVersion, s.deviceGeneration
	go func() {
		v, err := s.api.Call(s.ctx, "client.cached", nil, target, nil)
		s.cx.Post(func() {
			if s.ctx.Err() != nil || s.target != target || generation != s.deviceGeneration {
				return
			}
			s.cacheLoading = false
			// A slow disk result must never replace a newer remote catalogue.
			if err == nil && version == s.catalogueVersion {
				cached := object(v)
				if p := objects(cached["providers"]); len(p) > 0 {
					s.providers = visibleProviders(p)
				}
				if sessions := objects(cached["sessions"]); len(sessions) > 0 && len(s.sessions) == 0 {
					for _, session := range sessions {
						if visibleSession(session) {
							s.sessions = append(s.sessions, session)
						}
					}
				}
				s.rememberWorkspace()
			}
			s.changed()
		})
	}()
}

func (s *State) loadCachedSession() {
	key, generation := s.key(), s.sessionGeneration
	target, provider, session := s.target, str(s.selected, "providerId"), str(s.selected, "id")
	s.transcriptLoading = s.transcript == nil
	go func() {
		request := Object{"method": "transcript", "provider": provider, "session": session}
		v, err := s.api.Call(s.ctx, "client.cached", nil, target, request)
		s.cx.Post(func() {
			if s.ctx.Err() != nil || s.key() != key || s.sessionGeneration != generation {
				return
			}
			s.transcriptLoading = false
			if err == nil && v != nil && s.transcript == nil {
				cached := object(v)
				s.setTranscript(cached)
				// A disk baseline may arrive after the initial watch was launched.
				// Resume only before any remote baseline or snapshot has started.
				if resume := object(cached["resume"]); str(resume, "epoch") != "" && s.stream != nil && s.stream.current() && !s.stream.baseline && !s.stream.snapshotInFlight {
					s.streamKey, s.streamEpoch, s.streamCursor = key, str(resume, "epoch"), number(resume, "cursor")
					s.startSessionWatch()
				}
			} else if err == nil && v != nil {
				s.restoreOutgoing(object(v))
				s.reconcileOutgoing(time.Time{})
				s.rebuildMessages()
			}
			s.changed()
		})
	}()
	go func() {
		v, err := s.api.Call(s.ctx, "client.cached", nil, target, Object{"method": "models", "provider": provider, "session": session})
		s.cx.Post(func() {
			if s.ctx.Err() == nil && s.key() == key && s.sessionGeneration == generation && err == nil && len(s.models) == 0 {
				s.models = objects(v)
				s.changed()
			}
		})
	}()
}

func (s *State) cataloguePending() bool {
	return s.cacheLoading || s.connection(s.target).reconnecting || s.connected() && s.polling
}

func (s *State) emptyCatalogue(cx *core.Scope) core.Node {
	return core.Show{When: func() bool { s.read(); return len(s.sessions) == 0 }, Then: func(cx *core.Scope) core.Node {
		return core.View{TestID: "session-list-placeholder", Style: core.Style{Padding: core.All(24), Gap: 12, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Show{When: func() bool { s.read(); return s.cataloguePending() }, Then: func(*core.Scope) core.Node {
				return ui.Spinner{TestID: "sessions-loading", Label: "正在加载会话", Size: 18}
			}},
			dynamicText(cx, func() string {
				s.read()
				if s.cacheLoading {
					return "正在读取本地会话…"
				}
				if s.cataloguePending() {
					return "正在同步会话…"
				}
				if s.connected() {
					return "暂无会话，可以新建聊天。"
				}
				return "暂无本地会话，连接后会自动同步。"
			}, 12, true),
		}}
	}}
}

func (s *State) emptyTranscript(cx *core.Scope) core.Node {
	return core.Show{When: func() bool { s.read(); return s.selected != nil && s.transcript == nil }, Then: func(cx *core.Scope) core.Node {
		pending := func() bool {
			s.read()
			return s.transcriptLoading || s.transcriptSyncing || s.connection(s.target).reconnecting || s.stream != nil && s.stream.current() && s.stream.snapshotInFlight
		}
		return core.View{TestID: "transcript-placeholder", Style: core.Style{Position: core.Absolute, Top: core.Pct(40), Left: core.Pt(24), Right: core.Pt(24), Gap: 12, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Show{When: pending, Then: func(*core.Scope) core.Node {
				return ui.Spinner{TestID: "transcript-loading", Label: "正在加载消息", Size: 20}
			}},
			dynamicText(cx, func() string {
				if pending() {
					return "正在加载消息…"
				}
				if s.connected() {
					if s.stream != nil && s.stream.current() && s.stream.historyError != nil {
						_, detail := operationErrorText(s.stream.historyError.Error(), nil)
						return "消息加载失败：" + detail
					}
					return "消息尚未加载，请重试。"
				}
				return "此会话尚无本地消息，连接后会自动加载。"
			}, 13, true),
			core.Show{When: func() bool { return !pending() && s.connected() }, Then: func(cx *core.Scope) core.Node {
				return button(cx, "retry-transcript", "重试加载", nil, "outline", s.startSessionWatch)
			}},
		}}
	}}
}
