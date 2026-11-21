package assets

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/monkeyWie/reignhub/pkg/appdb"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Downloaded bodies are immutable private files. A transaction publishes their
// filenames and evicts the least recently used entries.
type Cache struct {
	Root, Database string
	Limit          int64
	mu             sync.Mutex
}

func (c *Cache) open() (*appdb.Handle, error) {
	path := c.Database
	if path == "" {
		path = appdb.Path(c.Root)
	}
	return appdb.Open(path)
}
func (c *Cache) Get(env, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	db, err := c.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var file string
	err = db.QueryRow("UPDATE download_cache SET touched=? WHERE env=? AND key=? RETURNING file", time.Now().UnixNano(), env, key).Scan(&file)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(c.Root, filepath.Base(file)))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}
func (c *Cache) Put(env, key string, b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit := c.Limit
	if limit <= 0 {
		limit = 128 << 20
	}
	if int64(len(b)) > min(limit, MaxFileSize) {
		return fmt.Errorf("附件超过缓存容量")
	}
	if err := os.MkdirAll(c.Root, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.Root, "download-*")
	if err != nil {
		return err
	}
	file := f.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(file)
		}
	}()
	_, err = f.Write(b)
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	db, err := c.open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var garbage []string
	var replaced string
	err = tx.QueryRow("SELECT file FROM download_cache WHERE env=? AND key=?", env, key).Scan(&replaced)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if replaced != "" {
		garbage = append(garbage, replaced)
	}
	_, err = tx.Exec("INSERT INTO download_cache VALUES(?,?,?,?,?) ON CONFLICT(env,key) DO UPDATE SET file=excluded.file,size=excluded.size,touched=excluded.touched", env, key, filepath.Base(file), len(b), time.Now().UnixNano())
	if err != nil {
		return err
	}
	var size int64
	if err = tx.QueryRow("SELECT coalesce(sum(size),0) FROM download_cache").Scan(&size); err != nil {
		return err
	}
	for size > limit {
		var name string
		var n int64
		err = tx.QueryRow("DELETE FROM download_cache WHERE (env,key)=(SELECT env,key FROM download_cache ORDER BY touched LIMIT 1) RETURNING file,size").Scan(&name, &n)
		if err != nil {
			return err
		}
		size -= n
		garbage = append(garbage, name)
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	return c.remove(garbage)
}
func (c *Cache) remove(files []string) error {
	for _, file := range files {
		err := os.Remove(filepath.Join(c.Root, filepath.Base(file)))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
func (c *Cache) Clear(env string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	db, err := c.open()
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.Query("DELETE FROM download_cache WHERE ?='' OR env=? RETURNING file", env, env)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		names = append(names, name)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	return c.remove(names)
}
func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
