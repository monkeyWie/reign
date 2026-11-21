package appdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/monkeyWie/reignhub/pkg/persistence"
)

func (h *Handle) LoadIdentity(service, role string, out any) error {
	var b []byte
	if err := h.QueryRow("SELECT value FROM identities WHERE service=? AND role=?", service, role).Scan(&b); err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (h *Handle) SaveIdentity(service, role string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = h.Exec("INSERT INTO identities VALUES(?,?,?) ON CONFLICT(service,role) DO UPDATE SET value=excluded.value", service, role, b)
	return err
}
func (h *Handle) LoadEnvironment(id string, out any) error {
	var b []byte
	if err := h.QueryRow("SELECT value FROM environments WHERE env=?", id).Scan(&b); err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (h *Handle) SaveEnvironment(id string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = h.Exec("INSERT INTO environments VALUES(?,?) ON CONFLICT(env) DO UPDATE SET value=excluded.value", id, b)
	return err
}
func (h *Handle) Environments() ([]string, error) {
	rows, err := h.Query("SELECT env FROM environments ORDER BY env")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (h *Handle) Route(ctx context.Context, peer string) (string, error) {
	tx, e := h.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var id string
	e = tx.QueryRowContext(ctx, "SELECT env FROM environment_routes WHERE peer=?", peer).Scan(&id)
	if e == nil {
		return id, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	var retired bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM forgotten WHERE env=?)", peer).Scan(&retired); e != nil {
		return "", e
	}
	id = peer
	if retired {
		id = peer + "|" + persistence.Revision()
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO environment_routes VALUES(?,?)", peer, id)
	if e != nil {
		return "", e
	}
	return id, tx.Commit()
}
