package store

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm/clause"
)

// openMem opens a fresh in-memory sqlite store for a test.
func openMem(t *testing.T) *DB {
	t.Helper()
	// A file in the temp dir (rather than :memory:) keeps a single shared
	// connection's schema visible across pooled connections without extra DSN
	// params, and is cleaned up automatically.
	db, err := Open(Options{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpenMigrateAllTables(t *testing.T) {
	db := openMem(t)
	for _, m := range AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Errorf("missing table for %T", m)
		}
	}
}

func TestDefaultsAndBadDriver(t *testing.T) {
	if _, err := Open(Options{Driver: "sqlite"}); err == nil {
		t.Error("empty DSN should error")
	}
	if _, err := Open(Options{Driver: "mysql", DSN: "x"}); err == nil {
		t.Error("unsupported driver should error")
	}
}

func TestViolationRoundTripAndUpsert(t *testing.T) {
	db := openMem(t)

	v := Violation{TS: 1763373946000, Keyword: "porn", URL: "https://x/?q=porn", Type: "url-keyword", Domain: "x"}
	if err := db.Create(&v).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	// Re-ingesting the same (TS, Keyword, URL) must not duplicate. An upsert that
	// does nothing on conflict keeps ingest idempotent for the syncer.
	dup := Violation{TS: v.TS, Keyword: v.Keyword, URL: v.URL, Type: "url-keyword", Domain: "x"}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&dup).Error; err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var n int64
	db.Model(&Violation{}).Count(&n)
	if n != 1 {
		t.Fatalf("want 1 violation after dup ingest, got %d", n)
	}

	var got Violation
	if err := db.First(&got, "keyword = ?", "porn").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Domain != "x" || got.TS != v.TS {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestUsageAndHeartbeatUniqueTS(t *testing.T) {
	db := openMem(t)
	if err := db.Create(&UsageSample{TS: 100, ActiveClass: "kitty", WindowCount: 2}).Error; err != nil {
		t.Fatalf("usage create: %v", err)
	}
	// Duplicate TS should conflict on the unique index; DoNothing swallows it.
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).
		Create(&UsageSample{TS: 100, ActiveClass: "firefox"}).Error; err != nil {
		t.Fatalf("usage upsert: %v", err)
	}
	var n int64
	db.Model(&UsageSample{}).Count(&n)
	if n != 1 {
		t.Errorf("want 1 usage sample, got %d", n)
	}

	if err := db.Create(&Heartbeat{TS: 200, Alive: true}).Error; err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
}
