package hubui

import (
	"encoding/json"
	"testing"
	"time"
)

func outgoingFixture() *State {
	return &State{target: "device", selected: Object{"providerId": "codex-app", "id": "chat"}, outgoing: map[string][]*outgoingMessage{}}
}
func outgoingTranscript(items ...Object) Object {
	return Object{"turns": []Object{{"id": "turn", "status": "completed", "items": items}}}
}
func TestOutgoingReconcilesNewEchoWithoutMatchingOldIdenticalText(t *testing.T) {
	s := outgoingFixture()
	a := &outgoingMessage{ID: "local-a", Text: "same", Status: "failed", Known: map[string]bool{"turn|old": true}}
	b := &outgoingMessage{ID: "local-b", Text: "same", Status: "sending", Known: map[string]bool{"turn|old": true}}
	s.outgoing[s.key()] = []*outgoingMessage{a, b}
	s.transcript = outgoingTranscript(Object{"id": "old", "role": "user", "text": "same"})
	s.reconcileOutgoing(time.Time{})
	if len(s.outgoing[s.key()]) != 2 {
		t.Fatal("matched an old identical message")
	}
	s.transcript = outgoingTranscript(Object{"id": "old", "role": "user", "text": "same"}, Object{"id": "new", "role": "user", "text": "same\n"})
	s.reconcileOutgoing(time.Time{})
	if len(s.outgoing[s.key()]) != 1 || s.outgoing[s.key()][0] != b {
		t.Fatal("echo was not merged exactly once")
	}
	s.reconcileOutgoing(time.Time{})
	if len(s.outgoing[s.key()]) != 1 {
		t.Fatal("repeated snapshot consumed a second pending message")
	}
	s.rebuildMessages()
	if len(s.messageRows) != 3 || s.messageRows[2].Delivery != "sending" {
		t.Fatal("pending overlay lost its status")
	}
}
func TestOnlyFreshSnapshotAfterRPCSettlesRemovesAbsentLocalMessage(t *testing.T) {
	s := outgoingFixture()
	now := time.Now()
	m := &outgoingMessage{ID: "local", Text: "missing", Status: "failed", FinishedAt: now}
	s.outgoing[s.key()] = []*outgoingMessage{m}
	s.transcript = outgoingTranscript()
	for _, started := range []time.Time{{}, now.Add(-time.Second)} {
		s.reconcileOutgoing(started)
		if len(s.outgoing[s.key()]) != 1 {
			t.Fatal("old/cache snapshot removed failed message")
		}
	}
	s.reconcileOutgoing(now.Add(time.Second))
	if len(s.outgoing[s.key()]) != 0 {
		t.Fatal("authoritatively absent message retained")
	}
	m.FinishedAt = time.Time{}
	m.Status = "sending"
	s.outgoing[s.key()] = []*outgoingMessage{m}
	s.reconcileOutgoing(now.Add(time.Second))
	if len(s.outgoing[s.key()]) != 1 {
		t.Fatal("in-flight message removed by concurrent sync")
	}
}
func TestOutgoingAttachmentEchoUsesContentAndFullText(t *testing.T) {
	s := outgoingFixture()
	m := &outgoingMessage{Text: "inspect", Attachments: []Object{{"id": "upload", "sha256": "digest"}}}
	s.outgoing[s.key()] = []*outgoingMessage{m}
	s.transcript = outgoingTranscript(Object{"id": "echo", "role": "user", "text": "inspect", "attachments": []Object{{"id": "imported", "sha256": "other"}}})
	s.reconcileOutgoing(time.Time{})
	if len(s.outgoing[s.key()]) != 1 {
		t.Fatal("matched different attachment")
	}
	s.transcript = outgoingTranscript(Object{"id": "echo", "role": "user", "text": "inspect", "attachments": []Object{{"id": "imported", "sha256": "digest"}}})
	s.reconcileOutgoing(time.Time{})
	if len(s.outgoing[s.key()]) != 0 {
		t.Fatal("failed to merge imported attachment")
	}
}
func TestOutgoingRestoresFromDiskAndIsolatesSessions(t *testing.T) {
	s := outgoingFixture()
	s.restoreOutgoing(nil)
	original := Object{"localMessages": []*outgoingMessage{{ID: "pending", Text: "saved", Status: "sending", Known: map[string]bool{"old": true}}}}
	raw, _ := json.Marshal(original)
	var decoded Object
	_ = json.Unmarshal(raw, &decoded)
	s.restoreOutgoing(decoded)
	m := s.outgoing[s.key()][0]
	if m.Text != "saved" || m.Status != "failed" || !m.Known["old"] {
		t.Fatal("pending disk snapshot was not restored", m)
	}
	key := s.key()
	s.selected = Object{"providerId": "codex-app", "id": "other"}
	s.reconcileOutgoing(time.Now())
	s.rebuildMessages()
	if len(s.messageRows) != 0 || len(s.outgoing[key]) != 1 {
		t.Fatal("local message leaked between sessions")
	}
}

func TestCachedAndPreviewCataloguesExcludeSDK(t *testing.T) {
	sessions := []Object{
		{"id": "sdk", "providerId": "codex-cli", "originator": "codex_sdk_ts", "source": `"appServer"`},
		{"id": "exec", "providerId": "codex-app", "sourceKind": "exec"},
		{"id": "cli", "providerId": "codex-cli", "sourceKind": "cli", "cwd": "/project"},
	}
	groups := directories(sessions)
	if len(groups) != 1 || len(groups[0].Sessions) != 1 || str(groups[0].Sessions[0], "id") != "cli" {
		t.Fatal("cached SDK sessions leaked", groups)
	}
}
