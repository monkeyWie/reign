package appdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (h *Handle) DirectoryPosition(ctx context.Context, k persistence.DirectoryKey) (persistence.DirectoryHead, error) {
	var p persistence.DirectoryHead
	err := h.QueryRowContext(ctx, "SELECT epoch,cursor FROM directory_heads WHERE env=? AND provider=?", k.Env, k.Provider).Scan(&p.Epoch, &p.Cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	return p, err
}
func (h *Handle) WriteDirectory(ctx context.Context, k persistence.DirectoryKey, old persistence.DirectoryHead, b persistence.DirectoryBatch) error {
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cur persistence.DirectoryHead
	err = tx.QueryRowContext(ctx, "SELECT epoch,cursor FROM directory_heads WHERE env=? AND provider=?", k.Env, k.Provider).Scan(&cur.Epoch, &cur.Cursor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if b.Preview && err == nil {
		return nil
	}
	if old != cur {
		return persistence.ErrConflict
	}
	if b.Reset {
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE env=? AND provider=?", k.Env, k.Provider); err != nil {
			return err
		}
	}
	for _, s := range b.Upserts {
		id, _ := s["id"].(string)
		if id == "" {
			return fmt.Errorf("empty session id")
		}
		v, e := json.Marshal(s)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO sessions VALUES(?,?,?,?) ON CONFLICT(env,provider,id) DO UPDATE SET value=excluded.value WHERE value!=excluded.value", k.Env, k.Provider, id, v); err != nil {
			return err
		}
	}
	for _, id := range b.Deletes {
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE env=? AND provider=? AND id=?", k.Env, k.Provider, id); err != nil {
			return err
		}
	}
	if !b.Preview {
		if _, err = tx.ExecContext(ctx, "INSERT INTO directory_heads VALUES(?,?,?,?) ON CONFLICT(env,provider) DO UPDATE SET epoch=excluded.epoch,cursor=excluded.cursor", k.Env, k.Provider, b.Head.Epoch, b.Head.Cursor); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (h *Handle) ListSessions(ctx context.Context, k persistence.DirectoryKey) ([]persistence.Object, error) {
	q := "SELECT value FROM sessions WHERE env=?"
	args := []any{k.Env}
	if k.Provider != "" {
		q += " AND provider=?"
		args = append(args, k.Provider)
	}
	rows, err := h.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []persistence.Object{}
	for rows.Next() {
		var raw []byte
		var s persistence.Object
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
