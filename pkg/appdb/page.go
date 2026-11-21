package appdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (h *Handle) ReadPage(ctx context.Context, k persistence.Session, q persistence.PageQuery) (persistence.HistoryPage, error) {
	var empty persistence.HistoryPage
	tx, err := h.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var meta []byte
	var rev string
	args := []any{k.Env, k.Provider, k.ID}
	err = tx.QueryRowContext(ctx, "SELECT revision,metadata FROM history_heads WHERE "+sessionWhere, args...).Scan(&rev, &meta)
	if k.Env == "local" && (err == nil || errors.Is(err, sql.ErrNoRows)) {
		var source []byte
		e := tx.QueryRowContext(ctx, "SELECT metadata FROM history_heads WHERE env='@managed' AND provider=? AND session=?", k.Provider, k.ID).Scan(&source)
		if e == nil {
			meta = source
			args[0] = "@managed"
			err = nil
		} else if !errors.Is(e, sql.ErrNoRows) {
			return empty, e
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var head persistence.Object
	if err = json.Unmarshal(meta, &head); err != nil {
		return empty, err
	}
	if k.Env == "local" && args[0] == "@managed" {
		delete(head, "nativeId")
		delete(head, "nextTurn")
		delete(head, "imported")
		if epoch, ok := head["streamEpoch"].(string); ok && epoch != "" {
			head["resume"] = persistence.Object{"epoch": epoch, "cursor": head["streamCursor"]}
		}
	}
	if q.Before == nil && q.After == nil {
		if err = loadOverlay(ctx, tx, k, head); err != nil {
			return empty, err
		}
	}
	b := persistence.NewPage(head, rev, q)
	stmt := "SELECT turn,position,metadata FROM turns WHERE " + sessionWhere
	turnArgs := append([]any{}, args...)
	bound, order, itemOp := q.Before, " DESC", "<"
	if q.After != nil {
		bound, order, itemOp = q.After, " ASC", ">"
	}
	if bound != nil {
		stmt += " AND position" + itemOp + "=?"
		turnArgs = append(turnArgs, bound.Turn)
	}
	turns, err := tx.QueryContext(ctx, stmt+" ORDER BY position"+order, turnArgs...)
	if err != nil {
		return empty, err
	}
	defer turns.Close()
	for turns.Next() {
		var id string
		var pos int
		var raw []byte
		if err = turns.Scan(&id, &pos, &raw); err != nil {
			return empty, err
		}
		ok, e := b.Turn(pos, raw)
		if e != nil {
			return empty, e
		}
		if !ok {
			break
		}
		stmt := "SELECT position,value FROM messages WHERE " + sessionWhere + " AND turn=?"
		itemArgs := append(append([]any{}, args...), id)
		if bound != nil && pos == bound.Turn {
			stmt += " AND position" + itemOp + "?"
			itemArgs = append(itemArgs, bound.Item)
		}
		rows, e := tx.QueryContext(ctx, stmt+" ORDER BY position"+order, itemArgs...)
		if e != nil {
			return empty, e
		}
		for rows.Next() {
			var n int
			if e = rows.Scan(&n, &raw); e == nil {
				ok, e = b.Item(n, raw)
			}
			if e != nil || !ok {
				break
			}
		}
		e = errors.Join(e, rows.Err(), rows.Close())
		if e != nil {
			return empty, e
		}
		if !ok {
			break
		}
		b.EndTurn(bound != nil && pos == bound.Turn)
	}
	return b.Finish(), turns.Err()
}
