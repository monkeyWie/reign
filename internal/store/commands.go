package store

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/monkeyWie/reignhub/internal/control"
)

// Execute reserves a command durably before invoking the external Agent. A crash
// in the external call window remains outcomeUnknown, never an automatic replay.
func (d *DB) Execute(r control.Request, run func() control.Response) control.Response {
	if r.ID == "" {
		return control.Response{Error: "commandId required"}
	}
	raw, _ := json.Marshal(r)
	digest := sha256.Sum256(raw)
	owner := r.ReceiptOwner
	if owner == "" {
		owner = "local"
	}
	d.mu.Lock()
	var previous, response []byte
	var state, storedOwner string
	e := d.QueryRow("SELECT owner,request,response,state FROM command_receipts WHERE id=?", r.ID).Scan(&storedOwner, &previous, &response, &state)
	if e == nil {
		d.mu.Unlock()
		if storedOwner != owner {
			return control.Response{ID: r.ID, Error: "commandId belongs to a different controller"}
		}
		if !bytes.Equal(previous, digest[:]) {
			return control.Response{ID: r.ID, Error: "commandId reused with different request"}
		}
		if state != "done" {
			return control.Response{ID: r.ID, Error: "outcomeUnknown: 请刷新原会话，不要重复提交"}
		}
		var out control.Response
		if json.Unmarshal(response, &out) != nil {
			return control.Response{ID: r.ID, Error: "invalid saved command response"}
		}
		return out
	}
	if e != sql.ErrNoRows {
		d.mu.Unlock()
		return control.Response{ID: r.ID, Error: e.Error()}
	}
	_, e = d.Exec("INSERT INTO command_receipts(owner,id,request,state) VALUES(?,?,?,?)", owner, r.ID, digest[:], "pending")
	d.mu.Unlock()
	if e != nil {
		return control.Response{ID: r.ID, Error: e.Error()}
	}
	out := run()
	b, _ := json.Marshal(out)
	if _, e = d.Exec("UPDATE command_receipts SET response=?,state='done' WHERE owner=? AND id=?", b, owner, r.ID); e != nil {
		return control.Response{ID: r.ID, Error: fmt.Sprintf("outcomeUnknown: %v", e)}
	}
	return out
}
