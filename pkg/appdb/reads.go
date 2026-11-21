package appdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (h *Handle) SaveList(ctx context.Context, k persistence.Session, kind persistence.ReadKind, list []persistence.Object) error {
	table := kind.Table()
	if table == "" {
		return fmt.Errorf("unknown read model")
	}
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO read_state VALUES(?,?,?,?)", k.Env, k.Provider, k.ID, kind); err != nil {
		return err
	}
	for pos, v := range list {
		b, e := json.Marshal(kind.Record(v))
		if e != nil {
			return e
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+table+" VALUES(?,?,?,?,?) ON CONFLICT(env,provider,session,position) DO UPDATE SET value=excluded.value WHERE value!=excluded.value", k.Env, k.Provider, k.ID, pos, b)
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+sessionWhere+" AND position>=?", k.Env, k.Provider, k.ID, len(list)); err != nil {
		return err
	}
	return tx.Commit()
}
func (h *Handle) LoadList(ctx context.Context, k persistence.Session, kind persistence.ReadKind) ([]persistence.Object, error) {
	table := kind.Table()
	if table == "" {
		return nil, nil
	}
	tx, err := h.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	args := []any{k.Env, k.Provider, k.ID}
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM read_state WHERE "+sessionWhere+" AND kind=?", append(args, kind)...).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT value FROM "+table+" WHERE "+sessionWhere+" ORDER BY position", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []persistence.Object{}
	for rows.Next() {
		var b []byte
		var obj persistence.Object
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &obj); err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, rows.Err()
}
