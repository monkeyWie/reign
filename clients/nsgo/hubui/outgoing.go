package hubui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *State) refreshAfterSend(key, target, provider, session string) {
	started, generation := time.Now(), s.sessionGeneration
	go func() {
		v, err := rpc(s.ctx, s.api, target, provider, session, "transcript", Object{"fresh": true})
		s.cx.Post(func() {
			if s.ctx.Err() != nil || s.key() != key || generation != s.sessionGeneration || err != nil {
				return
			}
			s.setTranscript(object(v))
			s.reconcileOutgoing(started)
			s.rebuildMessages()
			s.persistSnapshot()
			s.changed()
		})
	}()
}

// Local messages are an overlay, never synthetic Agent turns. Only a fresh
// remote snapshot may prove an unresolved message absent after an RPC settles.
type outgoingMessage struct {
	ID          string          `json:"id"`
	Text        string          `json:"text"`
	Status      string          `json:"status"`
	Error       string          `json:"error,omitempty"`
	Timestamp   int64           `json:"timestamp"`
	FinishedAt  time.Time       `json:"finishedAt"`
	Attachments []Object        `json:"attachments,omitempty"`
	Known       map[string]bool `json:"known"`
}

func (s *State) restoreOutgoing(t Object) {
	if s.outgoing == nil {
		s.outgoing = map[string][]*outgoingMessage{}
	}
	if _, exists := s.outgoing[s.key()]; exists {
		return
	}
	if _, exists := t["localMessages"]; !exists {
		return
	}
	var pending []*outgoingMessage
	b, _ := json.Marshal(t["localMessages"])
	_ = json.Unmarshal(b, &pending)
	for _, m := range pending {
		if m.Status == "sending" {
			m.Status, m.Error, m.FinishedAt = "failed", "发送结果尚未确认，连接恢复后将同步", time.Now()
		}
	}
	s.outgoing[s.key()] = pending
}

func (s *State) snapshotProjection() Object {
	t := copyObject(s.transcript)
	t["localMessages"] = s.outgoing[s.key()]
	return t
}

func (s *State) queueOutgoing(body string, uploads []*Upload) *outgoingMessage {
	s.restoreOutgoing(s.transcript)
	now := time.Now()
	m := &outgoingMessage{ID: fmt.Sprintf("local-%d", now.UnixNano()), Text: body, Status: "sending", Timestamp: now.UnixMilli(), Known: map[string]bool{}}
	for _, row := range messages(s.transcript) {
		if row.Role == "user" {
			m.Known[row.ID] = true
		}
	}
	for _, u := range uploads {
		kind := "file"
		if strings.HasPrefix(u.MIME, "image/") {
			kind = "image"
		}
		a := Object{"id": u.ID, "name": u.Name, "mime": u.MIME, "kind": kind, "size": len(u.Data), "sha256": hashBytes(u.Data), "state": "ready"}
		m.Attachments = append(m.Attachments, a)
		s.images[s.key()+"|"+u.ID+"|false"] = u.Data
	}
	s.outgoing[s.key()] = append(s.outgoing[s.key()], m)
	s.draft.Set("")
	s.uploads = nil
	s.rebuildMessages()
	s.persistSnapshot()
	s.timeline.ScrollToIndexAt(0, 1, 0, true)
	return m
}

func matchesOutgoing(m *outgoingMessage, row Message) bool {
	if row.Role != "user" || m.Known[row.ID] || strings.TrimSpace(displayText(row)) != m.Text {
		return false
	}
	if len(m.Attachments) != len(row.Attachments) {
		return false
	}
	// Attachment IDs change when Codex imports inputs; content hashes remain stable.
	used := map[int]bool{}
	for _, a := range m.Attachments {
		found := false
		for i, remote := range row.Attachments {
			if !used[i] && str(a, "sha256") != "" && str(a, "sha256") == str(remote, "sha256") {
				found, used[i] = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (s *State) reconcileOutgoing(freshSince time.Time) {
	if _, exists := s.outgoing[s.key()]; !exists {
		return
	}
	rows := messages(s.transcript)
	used := map[string]bool{}
	kept := []*outgoingMessage{}
	for _, m := range s.outgoing[s.key()] {
		found := false
		for _, row := range rows {
			if !used[row.ID] && matchesOutgoing(m, row) {
				used[row.ID], found = true, true
				for _, other := range s.outgoing[s.key()] {
					if other != m {
						if other.Known == nil {
							other.Known = map[string]bool{}
						}
						other.Known[row.ID] = true
					}
				}
				break
			}
		}
		if found {
			m.Status = "confirmed"
			continue
		}
		if !freshSince.IsZero() && !m.FinishedAt.IsZero() && !m.FinishedAt.After(freshSince) {
			continue
		}
		kept = append(kept, m)
	}
	s.outgoing[s.key()] = kept
}

func (s *State) rebuildMessages() {
	s.messageRows = messages(s.transcript)
	for _, m := range s.outgoing[s.key()] {
		s.messageRows = append(s.messageRows, Message{ID: m.ID, Role: "user", Text: m.Text, Timestamp: m.Timestamp, Attachments: m.Attachments, Delivery: m.Status, DeliveryError: m.Error})
	}
}
