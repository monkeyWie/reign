package appdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/monkeyWie/reignhub/pkg/persistence"
)

const sessionWhere = "env=? AND provider=? AND session=?"

func (h *Handle) ApplyHistory(ctx context.Context, k persistence.Session, p persistence.HistoryPatch) error {
	if err := p.Validate(k); err != nil {
		return err
	}
	head, err := json.Marshal(p.Head)
	if err != nil {
		return err
	}
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	args := []any{k.Env, k.Provider, k.ID}
	if k.Env == "local" {
		var managed bool
		if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM history_heads WHERE env='@managed' AND provider=? AND session=?)", k.Provider, k.ID).Scan(&managed); err != nil {
			return err
		}
		if managed {
			p.Turns = nil
			p.Messages = nil
			p.Replace = false
			head = []byte("{}")
		}
	}
	var result sql.Result
	if p.Base == "" {
		result, err = tx.ExecContext(ctx, "INSERT INTO history_heads VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING", k.Env, k.Provider, k.ID, p.Revision, head)
	} else {
		result, err = tx.ExecContext(ctx, "UPDATE history_heads SET revision=?,metadata=? WHERE "+sessionWhere+" AND revision=?", p.Revision, head, k.Env, k.Provider, k.ID, p.Base)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return persistence.ErrConflict
	}
	if p.Replace && p.Base != "" {
		tables := []string{"turns", "messages", persistence.Queued}
		if !p.PreserveOutgoing {
			tables = append(tables, persistence.Outgoing, persistence.Matches)
		}
		for _, table := range tables {
			if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+sessionWhere, args...); err != nil {
				return err
			}
		}
	}
	for _, r := range p.Turns {
		if r.Value == nil {
			for _, t := range []string{"messages", "turns"} {
				if _, err = tx.ExecContext(ctx, "DELETE FROM "+t+" WHERE "+sessionWhere+" AND turn=?", k.Env, k.Provider, k.ID, r.Turn); err != nil {
					return err
				}
			}
			continue
		}
		b, e := json.Marshal(r.Value)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO turns VALUES(?,?,?,?,?,?) ON CONFLICT(env,provider,session,turn) DO UPDATE SET position=excluded.position,metadata=excluded.metadata", k.Env, k.Provider, k.ID, r.Turn, r.Position, b); err != nil {
			return err
		}
	}
	for _, r := range p.Messages {
		if r.Value == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM messages WHERE "+sessionWhere+" AND turn=? AND item=?", k.Env, k.Provider, k.ID, r.Turn, r.Item)
		} else {
			b, e := json.Marshal(r.Value)
			if e != nil {
				return e
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO messages VALUES(?,?,?,?,?,?,?) ON CONFLICT(env,provider,session,turn,item) DO UPDATE SET position=excluded.position,value=excluded.value", k.Env, k.Provider, k.ID, r.Turn, r.Item, r.Position, b)
		}
		if err != nil {
			return err
		}
		if r.Value != nil {
			if r.Value["role"] == "user" {
				id, _ := r.Value["id"].(string)
				text, _ := r.Value["text"].(string)
				turn := strings.TrimPrefix(r.Turn, "id:")
				if k.Env == "@managed" {
					turn = r.Turn
				}
				_, err = tx.ExecContext(ctx, "INSERT INTO message_anchors VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(env,provider,session,turn,item) DO UPDATE SET row_id=excluded.row_id,preview=excluded.preview,position=excluded.position", k.Env, k.Provider, k.ID, r.Turn, r.Item, turn+"|"+id, persistence.Snippet(text), r.Position)
			} else {
				// Changes from user to another role are rare. An empty DELETE touches no rows.
				_, err = tx.ExecContext(ctx, "DELETE FROM message_anchors WHERE "+sessionWhere+" AND turn=? AND item=?", k.Env, k.Provider, k.ID, r.Turn, r.Item)
			}
			if err != nil {
				return err
			}
		}

	}
	if err = applyOverlay(ctx, tx, k, p.Overlay); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *Handle) ForgetHistory(ctx context.Context, env string) error {
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO forgotten VALUES(?)", env); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM environment_routes WHERE env=?", env); err != nil {
		return err
	}
	for _, table := range []string{"environments", "outgoing_messages", "queued_messages", "outgoing_matches", "messages", "turns", "history_heads", "sessions", "directory_heads", "providers", "models", "approval_previews", "read_state", "draft_files", "draft_blobs", "drafts"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE env=?", env); err != nil {
			return err
		}
	}
	return tx.Commit()
}
