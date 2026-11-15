package hubui

import (
	"context"
	"errors"
)

// A cold directory starts with one metadata page per provider. Full directory
// reconciliation runs independently; none of these paths requests transcripts.
func (s *State) loadCatalogue(ctx context.Context, cancel context.CancelFunc, generation uint64, target string, known map[string]bool) {
	defer cancel()
	current := func() bool { return s.ctx.Err() == nil && generation == s.pollGeneration && s.target == target }
	v, err := rpc(ctx, s.api, target, "", "", "providers", nil)
	providers := visibleProviders(objects(v))
	if err == nil {
		s.cx.Post(func() {
			if !current() {
				return
			}
			s.providers = providers
			visible := map[string]bool{}
			for _, p := range providers {
				visible[str(p, "id")] = true
			}
			kept := []Object{}
			for _, session := range s.sessions {
				if visible[str(session, "providerId")] {
					kept = append(kept, session)
				}
			}
			s.sessions = kept
			s.rememberWorkspace()
			s.changed()
		})
		results := make(chan error, len(providers))
		count := 0
		for _, p := range providers {
			if !providerReady(p) && !flag(object(p["capabilities"]), "history") {
				continue
			}
			providerID := str(p, "id")
			count++
			go func() {
				if !known[providerID] {
					page, previewErr := rpc(ctx, s.api, target, providerID, "", "sessions", nil)
					if previewErr == nil {
						sessions := objects(object(page)["sessions"])
						s.cx.Post(func() {
							if current() {
								s.applyCataloguePage(providerID, sessions, false)
							}
						})
					}
				}
				list, syncErr := s.api.Call(ctx, "client.sync", Object{"provider": providerID}, target, nil)
				if syncErr == nil {
					sessions := objects(list)
					s.cx.Post(func() {
						if current() {
							s.applyCataloguePage(providerID, sessions, true)
						}
					})
				}
				results <- syncErr
			}()
		}
		failures := []error{}
		for range count {
			if e := <-results; e != nil {
				failures = append(failures, e)
			}
		}
		err = errors.Join(failures...)
	}
	s.cx.Post(func() {
		if !current() {
			return
		}
		s.polling = false
		s.pollCancel = nil
		s.setConnectionIssue(target, "catalogue", err)
		s.changed()
		s.autoReconnect()
	})
}

func (s *State) applyCataloguePage(providerID string, incoming []Object, complete bool) {
	// An incomplete first page must not delete older cached sessions. The complete
	// incremental directory remains authoritative for deletions and reconciliation.
	merged := map[string]Object{}
	for _, session := range s.sessions {
		if !complete || str(session, "providerId") != providerID {
			merged[str(session, "providerId")+"|"+str(session, "id")] = session
		}
	}
	for _, session := range incoming {
		if str(session, "providerId") == providerID {
			merged[providerID+"|"+str(session, "id")] = session
		}
	}
	s.sessions = make([]Object, 0, len(merged))
	for _, session := range merged {
		s.sessions = append(s.sessions, session)
	}
	s.catalogueVersion++
	s.rememberWorkspace()
	s.changed()
}
