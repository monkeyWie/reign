package appdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/persistence"
	"slices"
)

func applyOverlay(ctx context.Context, tx *sql.Tx, k persistence.Session, changes []persistence.OverlayRow) error {
	for _, r := range changes {
		if !slices.Contains(persistence.OverlayTables, r.Table) {
			return fmt.Errorf("unknown history overlay")
		}
		var err error
		if r.Value == nil {
			_, err = tx.ExecContext(ctx, "DELETE FROM "+r.Table+" WHERE "+sessionWhere+" AND id=? AND ref=?", k.Env, k.Provider, k.ID, r.ID, r.Ref)
		} else {
			b, e := json.Marshal(r.Value)
			if e != nil {
				return e
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO "+r.Table+" VALUES(?,?,?,?,?,?,?) ON CONFLICT(env,provider,session,id,ref) DO UPDATE SET position=excluded.position,value=excluded.value", k.Env, k.Provider, k.ID, r.ID, r.Ref, r.Position, b)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func loadOverlay(ctx context.Context, tx *sql.Tx, k persistence.Session, head persistence.Object) error {
	var all []persistence.OverlayRow
	for _, table := range persistence.OverlayTables {
		rows, err := tx.QueryContext(ctx, "SELECT id,value FROM "+table+" WHERE "+sessionWhere+" ORDER BY position", k.Env, k.Provider, k.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var row persistence.OverlayRow
			var b []byte
			row.Table = table
			if err = rows.Scan(&row.ID, &b); err == nil {
				err = json.Unmarshal(b, &row.Value)
			}
			if err != nil {
				rows.Close()
				return err
			}
			all = append(all, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	persistence.RestoreOverlay(head, all)
	return nil
}
