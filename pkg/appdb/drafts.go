package appdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/composerdraft"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (h *Handle) ReadDraft(ctx context.Context, k persistence.Session) (composerdraft.Draft, error) {
	var d composerdraft.Draft
	tx, err := h.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	args := []any{k.Env, k.Provider, k.ID}
	err = tx.QueryRowContext(ctx, "SELECT text,model,base FROM drafts WHERE "+sessionWhere, args...).Scan(&d.Text, &d.Model, &d.ModelBase)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return d, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT name,mime,hash,size,content FROM draft_files JOIN draft_blobs USING(env,provider,session,hash) WHERE `+sessionWhere+" ORDER BY position", args...)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	for rows.Next() {
		var f composerdraft.File
		if err = rows.Scan(&f.Name, &f.MIME, &f.Hash, &f.Size, &f.Data); err != nil {
			return d, err
		}
		if len(f.Data) != f.Size || composerdraft.Hash(f.Data) != f.Hash {
			return d, fmt.Errorf("draft attachment integrity check failed")
		}
		d.Files = append(d.Files, f)
	}
	return d, rows.Err()
}
func (h *Handle) SaveDraft(ctx context.Context, k persistence.Session, d composerdraft.Draft) error {
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	args := []any{k.Env, k.Provider, k.ID}
	if d.Text == "" && d.Model == "" && len(d.Files) == 0 {
		_, err = tx.ExecContext(ctx, "DELETE FROM drafts WHERE "+sessionWhere, args...)
	} else {
		_, err = tx.ExecContext(ctx, "INSERT INTO drafts VALUES(?,?,?,?,?,?) ON CONFLICT(env,provider,session) DO UPDATE SET text=excluded.text,model=excluded.model,base=excluded.base", k.Env, k.Provider, k.ID, d.Text, d.Model, d.ModelBase)
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM draft_files WHERE "+sessionWhere+" AND position>=?", k.Env, k.Provider, k.ID, len(d.Files)); err != nil {
		return err
	}
	for i, f := range d.Files {
		if f.Data != nil || f.Size == 0 {
			data := f.Data
			if data == nil {
				data = []byte{}
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO draft_blobs VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING", k.Env, k.Provider, k.ID, f.Hash, data); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO draft_files VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(env,provider,session,position) DO UPDATE SET name=excluded.name,mime=excluded.mime,hash=excluded.hash,size=excluded.size WHERE (name,mime,hash,size)!=(excluded.name,excluded.mime,excluded.hash,excluded.size)`, k.Env, k.Provider, k.ID, i, f.Name, f.MIME, f.Hash, f.Size); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM draft_blobs WHERE "+sessionWhere+" AND hash NOT IN (SELECT hash FROM draft_files WHERE "+sessionWhere+")", append(args, args...)...); err != nil {
		return err
	}
	return tx.Commit()
}
