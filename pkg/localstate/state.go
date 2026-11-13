// Package localstate stores native application settings in SQLite. Browser
// clients intentionally retain IndexedDB rather than loading a second SQLite Wasm.
package localstate

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

func Open(path string) (*sql.DB, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = f.Close(); e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS settings (k TEXT PRIMARY KEY, v BLOB NOT NULL)"); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func Get(path, key string, out any) error {
	db, e := Open(path)
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
	db, e := Open(path)
	if e != nil {
		return e
	}
	defer db.Close()
	_, e = db.Exec("INSERT INTO settings(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v", key, b)
	return e
}
