// Package store supplies the native persistent cache and durable host change log.
package store

import (
	"encoding/json"
	"github.com/monkeyWie/reignhub/pkg/appdb"
	"sync"
)

type DB struct {
	*appdb.Handle
	mu sync.Mutex
}

func Open(path string) (*DB, error) {
	h, e := appdb.Open(path)
	if e != nil {
		return nil, e
	}
	return &DB{Handle: h}, nil
}
func (d *DB) Put(k string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = d.Exec("INSERT INTO settings VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", k, b)
	return e
}
func (d *DB) Get(k string, out any) error {
	var b []byte
	if e := d.QueryRow("SELECT v FROM settings WHERE k=?", k).Scan(&b); e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func (d *DB) Delete(k string) error { _, e := d.Exec("DELETE FROM settings WHERE k=?", k); return e }
func (d *DB) Keys(prefix string) ([]string, error) {
	rows, e := d.Query("SELECT k FROM settings WHERE substr(k,1,?)=? ORDER BY k", len(prefix), prefix)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	r := []string{}
	for rows.Next() {
		var k string
		if e = rows.Scan(&k); e != nil {
			return nil, e
		}
		r = append(r, k)
	}
	return r, rows.Err()
}
