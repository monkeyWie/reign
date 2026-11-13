// Package store supplies the native persistent cache and durable host change log.
package store

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
)

type DB struct {
	*sql.DB
	mu sync.Mutex
}

func Open(path string) (*DB, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	d, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	d.SetMaxOpenConns(1)
	if _, e = d.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS cache(k TEXT PRIMARY KEY, value BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS entities(scope TEXT NOT NULL,id TEXT NOT NULL,value BLOB NOT NULL,PRIMARY KEY(scope,id));
CREATE TABLE IF NOT EXISTS changes(seq INTEGER PRIMARY KEY AUTOINCREMENT,scope TEXT NOT NULL,id TEXT NOT NULL,value BLOB,deleted INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS commands(id TEXT PRIMARY KEY,request BLOB NOT NULL,response BLOB,state TEXT NOT NULL);`); e != nil {
		d.Close()
		return nil, e
	}
	os.Chmod(path, 0600)
	return &DB{DB: d}, nil
}
func (d *DB) Put(k string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = d.Exec("INSERT INTO cache VALUES(?,?) ON CONFLICT(k) DO UPDATE SET value=excluded.value", k, b)
	return e
}
func (d *DB) Get(k string, out any) error {
	var b []byte
	if e := d.QueryRow("SELECT value FROM cache WHERE k=?", k).Scan(&b); e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func (d *DB) Delete(k string) error { _, e := d.Exec("DELETE FROM cache WHERE k=?", k); return e }
func (d *DB) Keys(prefix string) ([]string, error) {
	rows, e := d.Query("SELECT k FROM cache WHERE substr(k,1,?)=? ORDER BY k", len(prefix), prefix)
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
