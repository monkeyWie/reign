package codex

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monkeyWie/reignhub/internal/codexipc"
	"github.com/monkeyWie/reignhub/internal/config"
	"github.com/monkeyWie/reignhub/internal/events"
	"github.com/monkeyWie/reignhub/internal/provider"
	"github.com/monkeyWie/reignhub/internal/rpc"
)

func queueFixtureMessages() []any {
	return []any{
		map[string]any{"id": "q1", "text": "follow up", "context": map[string]any{"attachments": []any{map[string]any{"type": "image", "path": `C:\private\photo.png`}}, "cwd": "/private/workspace"}},
		map[string]any{"id": "q2", "text": "follow up", "submission": map[string]any{"status": "outcome-unknown"}},
	}
}

func TestQueueNormalizationPreservesIdentityOrderAndSafePreview(t *testing.T) {
	q, err := normalizeQueue(rawJSON(queueFixtureMessages()))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Messages) != 2 || q.Messages[0].ID != "q1" || q.Messages[1].ID != "q2" || q.Messages[1].Status != "uncertain" {
		t.Fatal("queue identity/order lost", q)
	}
	a := q.Messages[0].Attachments[0]
	if a.Name != "photo.png" || a.Kind != "image" {
		t.Fatal(a)
	}
	if raw := string(rawJSON(q)); strings.Contains(raw, "private") || strings.Contains(raw, "context") {
		t.Fatal("private composer metadata escaped")
	}
	for _, raw := range []string{`null`, `{}`, `[{"id":"same"},{"id":"same"}]`, `[{"text":"missing id"}]`, `[{"id":"bad","text":12}]`, `[{"id":"bad","context":{"attachments":{}}}]`} {
		if _, err := normalizeQueue([]byte(raw)); err == nil {
			t.Fatal("invalid queue accepted", raw)
		}
	}
	if _, err := normalizeQueue(bytes.Repeat([]byte(" "), maxQueueBytes+1)); err == nil {
		t.Fatal("unbounded queue accepted")
	}
}

func writeQueueJSON(t *testing.T, home string, messages []any) []byte {
	t.Helper()
	raw := rawJSON(map[string]any{"queued-follow-ups": map[string]any{"selected": messages, "other": []any{map[string]any{"id": "other", "text": "another session"}}}})
	if err := os.WriteFile(filepath.Join(home, ".codex-global-state.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDesktopQueueBootstrapReadOnlyAndRecovery(t *testing.T) {
	home := t.TempDir()
	original := writeQueueJSON(t, home, queueFixtureMessages())
	q, err := readDesktopQueue(context.Background(), home, "selected")
	if err != nil || len(q.Messages) != 2 {
		t.Fatalf("queue: %+v %v", q, err)
	}
	if _, err = os.Stat(filepath.Join(home, ".codex-global-state.sqlite")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created SQLite")
	}
	after, _ := os.ReadFile(filepath.Join(home, ".codex-global-state.json"))
	if !bytes.Equal(original, after) {
		t.Fatal("read modified owner state")
	}
	q, err = readDesktopQueue(context.Background(), home, "absent")
	if err != nil || q.State != "ready" || len(q.Messages) != 0 {
		t.Fatal(q, err)
	}
	if err = os.WriteFile(filepath.Join(home, ".codex-global-state.json"), []byte(`{"incomplete":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readDesktopQueue(context.Background(), home, "selected"); err == nil {
		t.Fatal("partial write treated as empty")
	}
	writeQueueJSON(t, home, []any{})
	q, err = readDesktopQueue(context.Background(), home, "selected")
	if err != nil || len(q.Messages) != 0 {
		t.Fatal("recovery", q, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = readDesktopQueue(ctx, home, "selected"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
}

func TestDesktopQueueSQLiteSupersedesStaleJSON(t *testing.T) {
	home := t.TempDir()
	writeQueueJSON(t, home, queueFixtureMessages())
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex-global-state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE settings(scope TEXT,key TEXT,value TEXT,PRIMARY KEY(scope,key));PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	raw := string(rawJSON(map[string]any{"selected": []any{map[string]any{"id": "db", "text": "database queue"}}}))
	if _, err = db.Exec("INSERT INTO settings VALUES('global','queued-follow-ups',?)", raw); err != nil {
		t.Fatal(err)
	}
	q, err := readDesktopQueue(context.Background(), home, "selected")
	if err != nil || len(q.Messages) != 1 || q.Messages[0].ID != "db" {
		t.Fatal(q, err)
	}
	if _, err = db.Exec("UPDATE settings SET value='{}'"); err != nil {
		t.Fatal(err)
	}
	q, err = readDesktopQueue(context.Background(), home, "selected")
	if err != nil || len(q.Messages) != 0 {
		t.Fatal("stale JSON resurrected", q, err)
	}
	if _, err = db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	if _, err = readDesktopQueue(context.Background(), home, "selected"); err == nil {
		t.Fatal("unsupported SQLite fell back to stale JSON")
	}
}

func TestVSCodeQueueBootstrapRecoveryAndBounds(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "queued-follow-ups.json")
	raw := rawJSON(map[string]any{"selected": queueFixtureMessages(), "other": []any{map[string]any{"id": "private-other", "text": "other session"}}})
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(raw)
	q := readStagedQueue(context.Background(), home, []string{file}, "selected")
	if q.State != "ready" || len(q.Messages) != 2 || q.Messages[0].ID != "q1" || q.Messages[1].ID != "q2" {
		t.Fatalf("VS Code initial queue missing: %+v", q)
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("read changed VS Code storage", err)
	}
	writeQueueJSON(t, home, queueFixtureMessages())
	q = readStagedQueue(context.Background(), home, []string{file}, "selected")
	if q.State != "ready" || len(q.Messages) != 2 {
		t.Fatal("same IDs in App and VS Code duplicated", q)
	}
	for _, invalid := range [][]byte{[]byte(`{"selected":`), []byte(`null`), bytes.Repeat([]byte(" "), maxQueueBytes+1)} {
		write(invalid)
		q = readStagedQueue(context.Background(), home, []string{file}, "selected")
		if q.State != "unknown" || len(q.Messages) != 2 {
			t.Fatal("bad VS Code state erased known desktop entries", q)
		}
	}
	writeQueueJSON(t, home, []any{})
	write([]byte(`{"selected":[]}`))
	q = readStagedQueue(context.Background(), home, []string{file}, "selected")
	if q.State != "ready" || len(q.Messages) != 0 {
		t.Fatal("consumed queue did not recover", q)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	q = readStagedQueue(context.Background(), home, []string{file}, "selected")
	if q.State != "ready" || len(q.Messages) != 0 {
		t.Fatal("missing storage was not empty", q)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created vendor storage", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVSCodeQueue(ctx, file, "selected"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := readVSCodeQueue(context.Background(), home, "selected"); err == nil {
		t.Fatal("accepted a nonregular queue file")
	}
}

func TestStagedQueuesMergeOnlyMatchingIDsAndStayBounded(t *testing.T) {
	home := t.TempDir()
	writeQueueJSON(t, home, queueFixtureMessages())
	file := filepath.Join(t.TempDir(), "queued-follow-ups.json")
	messages := make([]any, 0, 100)
	for i := range 100 {
		messages = append(messages, map[string]any{"id": fmt.Sprintf("vscode-%d", i), "text": "follow up"})
	}
	if err := os.WriteFile(file, rawJSON(map[string]any{"selected": messages}), 0600); err != nil {
		t.Fatal(err)
	}
	q := readStagedQueue(context.Background(), home, []string{file}, "selected")
	if q.State != "unknown" || len(q.Messages) != 100 || q.Messages[2].ID != "vscode-0" {
		t.Fatal("union exceeded bounds or collapsed equal text", q)
	}
}

func TestIPCQueueSnapshotChangesAndOwnerIsolation(t *testing.T) {
	p := New(config.Config{}, "", events.New())
	backendFixture(t, p, func(m rpc.Message) (any, *rpc.Error) {
		if m.Method != "thread/queue/list" {
			t.Errorf("unexpected backend call: %s", m.Method)
		}
		return nil, &rpc.Error{Code: -32601, Message: "Unsupported method"}
	})
	f := attachIPCFixture(t, p, nil)
	writeQueueJSON(t, f.b.home, queueFixtureMessages())
	f.b.queueFiles = []string{filepath.Join(t.TempDir(), "queued-follow-ups.json")}
	if err := os.WriteFile(f.b.queueFiles[0], rawJSON(map[string]any{"selected": queueFixtureMessages()}), 0600); err != nil {
		t.Fatal(err)
	}
	f.b.mu.Lock()
	f.b.followed["selected"] = &followedThread{owner: "app-owner", changed: make(chan struct{}), state: rawJSON(map[string]any{"id": "selected", "turns": []any{}})}
	f.b.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, ok, err := p.ipcTranscript(ctx, "selected", "")
	if err != nil || !ok || out.Queue == nil || len(out.Queue.Messages) != 2 {
		t.Fatalf("initial queue missing: %+v %v", out, err)
	}
	publish := func(source, id string, version int, messages any) {
		f.b.Broadcast(codexipc.Message{Method: "thread-queued-followups-changed", Version: version, Source: source, Params: rawJSON(map[string]any{"hostId": "local", "conversationId": id, "messages": messages})})
	}
	epoch, cursor := p.stream().log.Position()
	for _, source := range []string{"other-owner", ""} {
		publish(source, "selected", 3, []any{})
	}
	publish("app-owner", "other-session", 3, []any{})
	publish("app-owner", "selected", 2, []any{})
	batch, err := p.stream().log.Probe(ctx, "selected", epoch, cursor)
	if err != nil || len(batch.Events) != 0 {
		t.Fatal("foreign queue accepted", batch, err)
	}
	publish("app-owner", "selected", 3, []any{})
	batch, err = p.stream().log.Probe(ctx, "selected", epoch, cursor)
	if err != nil || len(batch.Events) != 1 || batch.Events[0].Kind != "queue" || len(batch.Events[0].Queue.Messages) != 0 {
		t.Fatal(batch, err)
	}
	out, _, err = p.ipcTranscript(ctx, "selected", "")
	if err != nil || len(out.Queue.Messages) != 0 {
		t.Fatal("stale disk resurrected consumed queue", out, err)
	}
	publish("app-owner", "selected", 3, queueFixtureMessages())
	publish("app-owner", "selected", 3, map[string]any{})
	batch, _ = p.stream().log.Probe(ctx, "selected", epoch, cursor)
	last := batch.Events[len(batch.Events)-1]
	if last.Queue.State != "unknown" || len(last.Queue.Messages) != 2 {
		t.Fatal("invalid update erased queue", last)
	}
	out, _, err = p.ipcTranscript(ctx, "selected", "")
	if err != nil || out.Queue.State != "unknown" {
		t.Fatal("invalid live queue was replaced by a stale disk baseline", out, err)
	}
	publish("app-owner", "selected", 3, []any{})
	out, _, err = p.ipcTranscript(ctx, "selected", "")
	if err != nil || out.Queue.State != "ready" || len(out.Queue.Messages) != 0 {
		t.Fatal("queue did not recover", out, err)
	}
	f.b.Broadcast(codexipc.Message{Method: "client-status-changed", Params: rawJSON(map[string]any{"clientId": "app-owner", "status": "disconnected"})})
	batch, _ = p.stream().log.Probe(ctx, "selected", epoch, cursor)
	if batch.Events[len(batch.Events)-1].Queue.State != "unknown" {
		t.Fatal("owner disconnect retained current queue state")
	}
}

func TestQueueSurvivesProgressiveTranscriptAssembly(t *testing.T) {
	q, _ := normalizeQueue(rawJSON(queueFixtureMessages()))
	var history provider.TranscriptBuffer
	metadata := provider.Transcript{Queue: q}
	ctx := provider.WithTranscriptObserver(context.Background(), history.Add)
	if err := provider.ObserveTranscript(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	metadata.Queue = nil
	if err := provider.EmitTranscriptTurn(ctx, metadata, provider.Turn{ID: "turn", Items: []provider.Item{{ID: "answer", Text: strings.Repeat("x", 5000)}}}); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(history.Transcript)
	if history.Queue == nil || len(history.Queue.Messages) != 2 || !strings.Contains(string(encoded), "queue") {
		t.Fatal("queue lost during fragment assembly")
	}
}
