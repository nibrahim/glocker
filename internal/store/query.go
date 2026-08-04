package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ── Reads (all ordered by TS ascending, matching the dashboard's expectations) ──

func (db *DB) Violations() ([]Violation, error) {
	var out []Violation
	err := db.Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) Unblocks() ([]Unblock, error) {
	var out []Unblock
	err := db.Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) LifecycleEvents() ([]LifecycleEvent, error) {
	var out []LifecycleEvent
	err := db.Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) UsageSamples() ([]UsageSample, error) {
	var out []UsageSample
	err := db.Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) Heartbeats() ([]Heartbeat, error) {
	var out []Heartbeat
	err := db.Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) Rules() ([]Rule, error) {
	var out []Rule
	err := db.Order("id asc").Find(&out).Error
	return out, err
}

func (db *DB) Colors() (map[string]string, error) {
	var rows []TagColor
	if err := db.Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Tag] = r.Color
	}
	return m, nil
}

func (db *DB) IgnoredViolations() ([]IgnoredViolation, error) {
	var out []IgnoredViolation
	err := db.Order("ts asc").Find(&out).Error
	return out, err
}

// ── Ingest (idempotent: safe for the syncer's one-shot backfill + repeated
// incremental pushes; overlapping records collapse on their natural key) ──

// IngestViolations upserts report lines; a re-sent (ts,keyword,url) is a no-op.
func (db *DB) IngestViolations(rows []Violation) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ts"}, {Name: "keyword"}, {Name: "url"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

// IngestUnblocks upserts unblock events. On (ts,domain) conflict it updates
// restore_ts and reason, so a later sync can close an unblock that was still
// open when first sent.
func (db *DB) IngestUnblocks(rows []Unblock) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ts"}, {Name: "domain"}},
		DoUpdates: clause.AssignmentColumns([]string{"restore_ts", "reason"}),
	}).CreateInBatches(rows, 500).Error
}

func (db *DB) IngestLifecycle(rows []LifecycleEvent) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ts"}, {Name: "type"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

func (db *DB) IngestUsage(rows []UsageSample) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ts"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

func (db *DB) IngestHeartbeats(rows []Heartbeat) error {
	if len(rows) == 0 {
		return nil
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ts"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

// ── Dashboard-local settings (replace-all, matching the old PUT semantics) ──

// SetRulesConfig replaces the entire rules + colours set in one transaction.
func (db *DB) SetRulesConfig(rules []Rule, colors map[string]string) error {
	return db.Transaction(func(tx *DB) error {
		if err := tx.Where("1 = 1").Delete(&Rule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&TagColor{}).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			if err := tx.Create(&rules).Error; err != nil {
				return err
			}
		}
		if len(colors) > 0 {
			tc := make([]TagColor, 0, len(colors))
			for tag, color := range colors {
				tc = append(tc, TagColor{Tag: tag, Color: color})
			}
			if err := tx.Create(&tc).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SetIgnored replaces the entire false-positive ignore list.
func (db *DB) SetIgnored(rows []IgnoredViolation) error {
	return db.Transaction(func(tx *DB) error {
		if err := tx.Where("1 = 1").Delete(&IgnoredViolation{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			return tx.Create(&rows).Error
		}
		return nil
	})
}

// Transaction runs fn inside a DB transaction, wrapping the *gorm.DB back into
// our *DB so the helper methods are available on the tx handle.
func (db *DB) Transaction(fn func(tx *DB) error) error {
	return db.DB.Transaction(func(g *gorm.DB) error {
		return fn(&DB{g})
	})
}

// Counts returns row counts per table (for the health endpoint).
func (db *DB) Counts() map[string]int64 {
	out := map[string]int64{}
	for name, m := range map[string]any{
		"violations": &Violation{}, "unblocks": &Unblock{},
		"lifecycle": &LifecycleEvent{}, "usage": &UsageSample{},
		"heartbeat": &Heartbeat{}, "rules": &Rule{}, "ignored": &IgnoredViolation{},
	} {
		var n int64
		db.Model(m).Count(&n)
		out[name] = n
	}
	return out
}
