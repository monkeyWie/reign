// Package database configures the default pure-Go SQLite database. Services can
// instead supply any GORM Dialector (for example PostgreSQL) to gorm.Open.
package database

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
)

func OpenSQLite(path string) (*gorm.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// Create with restricted permissions before SQLite creates journal files.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	db, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	raw, err := db.DB()
	if err != nil {
		return nil, err
	}
	raw.SetMaxOpenConns(1)
	if err = db.Exec("PRAGMA journal_mode=WAL").Error; err == nil {
		err = db.Exec("PRAGMA busy_timeout=5000").Error
	}
	if err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return db, nil
}

// OpenSQLiteReadOnly opens existing service state without initializing its schema or identity.
func OpenSQLiteReadOnly(path string) (*gorm.DB, error) {
	if path == "" {
		return nil, os.ErrInvalid
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	file := filepath.ToSlash(absolute)
	if !strings.HasPrefix(file, "/") {
		file = "/" + file
	}
	uri := url.URL{Scheme: "file", Path: file, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	return openSQLite(uri.String())
}

func openSQLite(dsn string) (*gorm.DB, error) {
	return gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: dsn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
}
