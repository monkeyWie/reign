// Package localstate stores native application settings in SQLite. Browser
// clients intentionally retain IndexedDB rather than loading a second SQLite Wasm.
package localstate

import (
	"encoding/json"
	"github.com/monkeyWie/reignhub/pkg/appdb"
)

func Get(path, key string, out any) error {
	db, e := appdb.Open(path)
	if e != nil {
		return e
	}
	defer db.Close()
	var b []byte
	if e = db.QueryRow("SELECT v FROM settings WHERE k=?", key).Scan(&b); e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func Put(path, key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	db, e := appdb.Open(path)
	if e != nil {
		return e
	}
	defer db.Close()
	_, e = db.Exec("INSERT INTO settings(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", key, b)
	return e
}
