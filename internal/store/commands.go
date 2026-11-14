package store

import (
	"bytes"
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
	d.mu.Lock()
	var previous, response []byte
	var state string
	e := d.QueryRow("SELECT request,response,state FROM commands WHERE id=?", r.ID).Scan(&previous, &response, &state)
	if e == nil {
		d.mu.Unlock()
		if !bytes.Equal(previous, raw) {
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
	_, e = d.Exec("INSERT INTO commands(id,request,state) VALUES(?,?,?)", r.ID, raw, "pending")
	d.mu.Unlock()
	if e != nil {
		return control.Response{ID: r.ID, Error: e.Error()}
	}
	out := run()
	b, _ := json.Marshal(out)
	if _, e = d.Exec("UPDATE commands SET response=?,state='done' WHERE id=?", b, r.ID); e != nil {
		return control.Response{ID: r.ID, Error: fmt.Sprintf("outcomeUnknown: %v", e)}
	}
	return out
}
