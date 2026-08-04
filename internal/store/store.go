package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite" // pure-Go (modernc) sqlite driver — no cgo
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Options selects the backend. Driver is "sqlite" (default) or "postgres";
// empty fields fall back to the config defaults. For sqlite, DSN is a file path.
type Options struct {
	Driver string
	DSN    string
}

// DB wraps the GORM handle. Callers use the embedded *gorm.DB directly; the
// wrapper exists so we can add helpers (ingest, queries) without leaking GORM
// everywhere later.
type DB struct {
	*gorm.DB
}

// Open connects using the given driver/DSN and runs migrations. The dialector is
// the only dialect-specific choice — everything above this is portable GORM, so
// swapping sqlite<->postgres is contained here.
func Open(o Options) (*DB, error) {
	driver := o.Driver
	if driver == "" {
		driver = "sqlite"
	}
	dsn := o.DSN
	if dsn == "" {
		return nil, fmt.Errorf("store: empty DSN for driver %q", driver)
	}

	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		// Ensure the parent dir exists for a file DSN (skip in-memory).
		if !strings.HasPrefix(dsn, ":memory:") && !strings.Contains(dsn, "mode=memory") {
			if dir := filepath.Dir(dsn); dir != "" && dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, fmt.Errorf("store: creating db dir %s: %w", dir, err)
				}
			}
		}
		dialector = sqlite.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("store: unsupported driver %q (want sqlite or postgres)", driver)
	}

	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", driver, err)
	}

	db := &DB{gdb}
	if err := db.Migrate(); err != nil {
		return nil, err
	}
	return db, nil
}

// Migrate creates/updates all tables. Safe to call repeatedly (idempotent).
func (db *DB) Migrate() error {
	if err := db.AutoMigrate(AllModels()...); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// Close releases the underlying connection pool.
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
