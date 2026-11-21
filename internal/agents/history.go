package agents

import (
	"context"
	"encoding/json"

	"github.com/monkeyWie/reignhub/pkg/appdb"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (p *Provider) historyKey(id string) persistence.Session {
	return persistence.Session{Env: "@managed", Provider: p.id, ID: id}
}
func (p *Provider) restoreDirectory(ctx context.Context) error {
	db, err := appdb.Open(p.path())
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT h.session,h.revision,h.metadata,r.session IS NOT NULL FROM history_heads h LEFT JOIN removed_sessions r ON r.provider=h.provider AND r.session=h.session WHERE h.env='@managed' AND h.provider=?`, p.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	p.removed = map[string]bool{}
	for rows.Next() {
		var id, revision string
		var raw []byte
		var removed bool
		if err := rows.Scan(&id, &revision, &raw, &removed); err != nil {
			return err
		}
		if removed {
			p.removed[id] = true
		}
		if p.sessions[id] != nil {
			continue
		}
		var ref struct {
			sessionRef
			NextTurn int
		}
		if err = json.Unmarshal(raw, &ref); err != nil {
			return err
		}
		s := &session{NativeID: ref.NativeID, Imported: ref.Imported, persisted: true, revision: revision, start: ref.NextTurn}
		s.Session = ref.Session
		if s.Session.ActiveTurnID != "" {
			s.Session.Status = "interrupted"
			s.Session.ActiveTurnID = ""
		}
		p.sessions[id] = s
	}
	return rows.Err()
}
func (s *session) mark(turn, item int) {
	if s.dirty == nil {
		s.dirty = map[int]map[int]bool{}
	}
	if s.dirty[turn] == nil {
		s.dirty[turn] = map[int]bool{}
	}
	s.dirty[turn][item] = true
}
func fields(v any) (persistence.Object, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out persistence.Object
	err = json.Unmarshal(raw, &out)
	return out, err
}

// Called with p.mu held. Only dirty item bodies cross the encoding boundary.
func (p *Provider) historyPatch(s *session) (persistence.HistoryPatch, error) {
	meta, err := fields(sessionRef{s.Session, s.NativeID, s.Imported})
	if err != nil {
		return persistence.HistoryPatch{}, err
	}
	meta["nextTurn"] = s.start + len(s.Turns)
	meta["streamEpoch"], meta["streamCursor"] = p.log.Position()
	patch := persistence.HistoryPatch{Base: s.revision, Revision: persistence.Revision(), Head: meta, Replace: s.replace}
	for n, items := range s.dirty {
		turn := s.Turns[n]
		turn.Items = nil
		header, err := fields(turn)
		if err != nil {
			return patch, err
		}
		delete(header, "items")
		if items[-1] || s.replace || !s.persisted || items[0] {
			patch.Turns = append(patch.Turns, persistence.Row{Turn: turn.ID, Position: s.start + n, Value: header})
		}
		for i := range items {
			if i < 0 {
				continue
			}
			item := s.Turns[n].Items[i]
			body, err := fields(item)
			if err != nil {
				return patch, err
			}
			patch.Messages = append(patch.Messages, persistence.Row{Turn: turn.ID, Item: item.ID, Position: i, Value: body})
		}
	}
	return patch, nil
}
func (p *Provider) persist(id string) error {
	p.persistMu.Lock()
	defer p.persistMu.Unlock()
	p.mu.Lock()
	s := p.sessions[id]
	if s == nil || s.Turns == nil || p.removed[id] {
		p.mu.Unlock()
		return nil
	}
	patch, err := p.historyPatch(s)
	if err != nil {
		p.mu.Unlock()
		return err
	}
	dirty := s.dirty
	s.dirty = nil
	s.replace = false
	p.mu.Unlock()
	db, err := appdb.Open(p.path())
	if err == nil {
		err = db.ApplyHistory(context.Background(), p.historyKey(id), patch)
		db.Close()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if !s.replace {
			for n, items := range dirty {
				for i := range items {
					s.mark(n, i)
				}
			}
			s.replace = patch.Replace
		}
		return err
	}
	s.persisted = true
	s.revision = patch.Revision
	if !s.sending && len(s.dirty) == 0 && !s.replace {
		s.start += len(s.Turns)
		s.Turns = nil
	}
	return nil
}
