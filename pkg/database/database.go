// Package database configures the default pure-Go SQLite database. Services can
// instead supply any GORM Dialector (for example PostgreSQL) to gorm.Open.
package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
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
	db, err := gorm.Open(sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: path}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
