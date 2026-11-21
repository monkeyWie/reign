package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
)

type Change struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Value   json.RawMessage `json:"value,omitempty"`
	Deleted bool            `json:"deleted"`
}
type Delta struct {
	Epoch   string            `json:"epoch"`
	Cursor  int64             `json:"cursor"`
	Reset   bool              `json:"reset"`
	Items   []json.RawMessage `json:"items,omitempty"`
	Changes []Change          `json:"changes"`
}

// Replace materializes the complete bounded directory and its change log atomically.
// External providers may change while paginating; the next refresh reconciles those changes.
func (d *DB) Replace(scope string, items map[string]json.RawMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, e := d.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	old := map[string][]byte{}
	rows, e := tx.Query("SELECT id,value FROM host_sessions WHERE scope=?", scope)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id string
		var b []byte
		if e = rows.Scan(&id, &b); e != nil {
			rows.Close()
			return e
		}
		old[id] = b
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for id, b := range items {
		if bytes.Equal(old[id], b) {
			delete(old, id)
			continue
		}
		if _, e = tx.Exec("INSERT INTO host_sessions VALUES(?,?,?) ON CONFLICT(scope,id) DO UPDATE SET value=excluded.value", scope, id, []byte(b)); e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO directory_changes(scope,id,value) VALUES(?,?,?)", scope, id, []byte(b)); e != nil {
			return e
		}
		delete(old, id)
	}
	for id := range old {
		if _, e = tx.Exec("DELETE FROM host_sessions WHERE scope=? AND id=?", scope, id); e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT INTO directory_changes(scope,id,deleted) VALUES(?,?,1)", scope, id); e != nil {
			return e
		}
	}
	// Bound the change log. Older client cursors receive a full reset.
	if _, e = tx.Exec("DELETE FROM directory_changes WHERE seq < (SELECT COALESCE(MAX(seq),0)-10000 FROM directory_changes)"); e != nil {
		return e
	}
	return tx.Commit()
}
func (d *DB) Delta(scope, epoch string, after int64) (Delta, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := Delta{Epoch: epoch, Changes: []Change{}}
	tx, e := d.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = tx.QueryRow("SELECT COALESCE(MAX(seq),0) FROM directory_changes").Scan(&out.Cursor); e != nil {
		return out, e
	}
	var floor int64
	if e = tx.QueryRow("SELECT COALESCE(MIN(seq),0) FROM directory_changes").Scan(&floor); e != nil {
		return out, e
	}
	if after > out.Cursor || (floor > 1 && after < floor) {
		after = 0
	}
	if after == 0 {
		out.Reset = true
		out.Items = []json.RawMessage{}
		rows, e := tx.Query("SELECT value FROM host_sessions WHERE scope=? ORDER BY id", scope)
		if e != nil {
			return out, e
		}
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				rows.Close()
				return out, e
			}
			out.Items = append(out.Items, json.RawMessage(b))
		}
		e = rows.Err()
		rows.Close()
		return out, e
	}
	rows, e := tx.Query("SELECT seq,id,value,deleted FROM directory_changes WHERE scope=? AND seq>? ORDER BY seq LIMIT 1000", scope, after)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var c Change
		var b []byte
		if e = rows.Scan(&c.Seq, &c.ID, &b, &c.Deleted); e != nil {
			return out, e
		}
		c.Value = b
		out.Changes = append(out.Changes, c)
	}
	if len(out.Changes) == 1000 {
		out.Cursor = out.Changes[999].Seq
	}
	return out, rows.Err()
}
