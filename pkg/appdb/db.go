// Package appdb owns the native application's private SQLite connection pool.
package appdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const Filename = "reignhub.db"

func Path(dir string) string { return filepath.Join(dir, Filename) }

type pool struct {
	db     *sql.DB
	refs   int
	cancel context.CancelFunc
}

var pools = struct {
	sync.Mutex
	byPath map[string]*pool
}{byPath: map[string]*pool{}}

type Handle struct {
	*sql.DB
	path string
	once sync.Once
	err  error
}

func Open(path string) (*Handle, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	pools.Lock()
	defer pools.Unlock()
	p := pools.byPath[path]
	if p == nil {
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
		if err = os.Chmod(path, 0600); err != nil {
			return nil, err
		}
		uri := filepath.ToSlash(path)
		if !strings.HasPrefix(uri, "/") {
			uri = "/" + uri
		}
		// Reserve the writer before reading ownership or a checkpoint. In WAL
		// mode a deferred read transaction cannot wait to upgrade a stale snapshot.
		u := url.URL{Scheme: "file", Path: uri, RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"}
		db, err := sql.Open("sqlite", u.String())
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(4)
		if _, err = db.Exec(schema); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize application database: %w", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		p = &pool{db: db, cancel: cancel}
		go maintain(ctx, db)
		pools.byPath[path] = p
	}
	p.refs++
	return &Handle{DB: p.db, path: path}, nil
}
func (h *Handle) Close() error {
	h.once.Do(func() {
		pools.Lock()
		defer pools.Unlock()
		p := pools.byPath[h.path]
		p.refs--
		if p.refs == 0 {
			delete(pools.byPath, h.path)
			p.cancel()
			h.err = p.db.Close()
		}
	})
	return h.err
}

// Each maintenance pass frees at most 64 unused pages and yields the writer.
func maintain(ctx context.Context, db *sql.DB) {
	timer := time.NewTicker(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			work, cancel := context.WithTimeout(ctx, time.Second)
			_, _ = db.ExecContext(work, "PRAGMA incremental_vacuum(64)")
			cancel()
		}
	}
}
