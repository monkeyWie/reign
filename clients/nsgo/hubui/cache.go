package hubui

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
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
	s.sessionCache[key] = s.transcript
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
						if str(session, "providerId") != "codex-vscode" {
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
				s.setTranscript(object(v))
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
	return core.Dynamic(func(cx *core.Scope) core.Node {
		s.read()
		if len(s.sessions) > 0 {
			return core.View{}
		}
		children := []core.Node{}
		text := "暂无本地会话，连接后会自动同步。"
		if s.cataloguePending() {
			children = append(children, ui.Spinner{TestID: "sessions-loading", Label: "正在加载会话", Size: 18})
			text = "正在同步会话…"
			if s.cacheLoading {
				text = "正在读取本地会话…"
			}
		} else if s.connected() {
			text = "暂无会话，可以新建聊天。"
		}
		children = append(children, label(cx, text, 12, true))
		return core.View{TestID: "session-list-placeholder", Style: core.Style{Padding: core.All(24), Gap: 12, AlignItems: core.AlignCenter}, Children: children}
	})
}

func (s *State) emptyTranscript(cx *core.Scope) core.Node {
	return core.Show{When: func() bool { s.read(); return s.selected != nil && s.transcript == nil }, Then: func(cx *core.Scope) core.Node {
		return core.Dynamic(func(cx *core.Scope) core.Node {
			s.read()
			children := []core.Node{}
			text := "此会话尚无本地消息，连接后会自动加载。"
			if s.transcriptLoading || s.transcriptSyncing || s.connection(s.target).reconnecting {
				children = append(children, ui.Spinner{TestID: "transcript-loading", Label: "正在加载消息", Size: 20})
				text = "正在加载消息…"
			}
			children = append(children, label(cx, text, 13, true))
			return core.View{TestID: "transcript-placeholder", Style: core.Style{Position: core.Absolute, Top: core.Pct(40), Left: core.Pt(24), Right: core.Pt(24), Gap: 12, AlignItems: core.AlignCenter}, Children: children}
		})
	}}
}
