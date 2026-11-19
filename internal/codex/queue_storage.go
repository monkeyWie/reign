package codex

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/monkeyWie/reignhub/internal/provider"
	_ "modernc.org/sqlite"
)

// Read only; never create, migrate or write Codex's storage. Once SQLite is
// initialized it is authoritative and the JSON export may be obsolete.
func readDesktopQueue(ctx context.Context, home, id string) (*provider.MessageQueue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file := filepath.Join(home, ".codex-global-state.sqlite")
	if _, err := os.Stat(file); err == nil {
		uriPath := filepath.ToSlash(file)
		if !strings.HasPrefix(uriPath, "/") {
			uriPath = "/" + uriPath
		}
		u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(50)"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			return nil, err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		var version int
		if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return nil, err
		}
		if version != 0 {
			if version != 1 {
				return nil, errors.New("unsupported Codex global state version")
			}
			var raw string
			err = db.QueryRowContext(ctx, "SELECT CASE WHEN length(CAST(value AS BLOB)) <= ? THEN value ELSE NULL END FROM settings WHERE scope = 'global' AND key = 'queued-follow-ups'", maxQueueBytes).Scan(&raw)
			if errors.Is(err, sql.ErrNoRows) {
				return normalizeQueue(json.RawMessage("[]"))
			}
			if err != nil {
				return nil, err
			}
			return desktopQueueEntry(ctx, []byte(raw), id)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.Open(filepath.Join(home, ".codex-global-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return normalizeQueue(json.RawMessage("[]"))
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const limit = 16 << 20
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, errors.New("Codex global state exceeds read limit")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var state map[string]json.RawMessage
	if err = json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, errors.New("invalid Codex global state")
	}
	queue, ok := state["queued-follow-ups"]
	if !ok {
		return normalizeQueue(json.RawMessage("[]"))
	}
	return desktopQueueEntry(ctx, queue, id)
}

func desktopQueueEntry(ctx context.Context, raw []byte, id string) (*provider.MessageQueue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var state map[string]json.RawMessage
	if len(raw) > maxQueueBytes || json.Unmarshal(raw, &state) != nil || state == nil {
		return nil, errors.New("invalid Codex queue state")
	}
	messages, ok := state[id]
	if !ok {
		messages = json.RawMessage("[]")
	}
	queue, err := normalizeQueue(messages)
	if cancelled := ctx.Err(); cancelled != nil {
		return nil, cancelled
	}
	return queue, err
}

// Bootstrap from known local staging stores; the creation client's sourceKind
// does not identify the current owner. A live owner broadcast supersedes this
// entire bootstrap, including an explicit empty queue after consumption.
func readStagedQueue(ctx context.Context, home string, files []string, id string) *provider.MessageQueue {
	out := &provider.MessageQueue{State: "ready", Messages: []provider.QueuedMessage{}}
	seen := map[string]bool{}
	appendQueue := func(q *provider.MessageQueue, err error) {
		if err != nil || q == nil {
			out.State = "unknown"
			return
		}
		for _, m := range q.Messages {
			if seen[m.ID] {
				continue
			}
			if len(out.Messages) >= 100 {
				out.State = "unknown"
				return
			}
			seen[m.ID] = true
			out.Messages = append(out.Messages, m)
		}
	}
	appendQueue(readDesktopQueue(ctx, home, id))
	for _, file := range files {
		if ctx.Err() != nil {
			out.State = "unknown"
			break
		}
		appendQueue(readVSCodeQueue(ctx, file, id))
	}
	return out
}

func readVSCodeQueue(ctx context.Context, file, id string) (*provider.MessageQueue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return normalizeQueue(json.RawMessage("[]"))
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxQueueBytes {
		return nil, errors.New("invalid VS Code queue file")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Check cancellation between bounded chunks. OS file reads themselves cannot
	// be interrupted; only known regular local files are considered.
	raw := make([]byte, 0, min(info.Size(), maxQueueBytes))
	buf := make([]byte, 32<<10)
	for len(raw) <= maxQueueBytes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := f.Read(buf)
		raw = append(raw, buf[:n]...)
		if err == io.EOF {
			return desktopQueueEntry(ctx, raw, id)
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("VS Code queue exceeds read limit")
}
