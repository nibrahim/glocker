package store

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// All reads/writes below are scoped to a single account (userID). Ingest sets
// UserID on every row from the authenticated token; reads filter by it.

// ── Reads (ordered by TS ascending, scoped to userID) ──

func (db *DB) Violations(userID uint) ([]Violation, error) {
	var out []Violation
	err := db.Where("user_id = ?", userID).Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) Unblocks(userID uint) ([]Unblock, error) {
	var out []Unblock
	err := db.Where("user_id = ?", userID).Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) LifecycleEvents(userID uint) ([]LifecycleEvent, error) {
	var out []LifecycleEvent
	err := db.Where("user_id = ?", userID).Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) UsageSamples(userID uint) ([]UsageSample, error) {
	var out []UsageSample
	err := db.Where("user_id = ?", userID).Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) Heartbeats(userID uint) ([]Heartbeat, error) {
	var out []Heartbeat
	err := db.Where("user_id = ?", userID).Order("ts asc").Find(&out).Error
	return out, err
}

func (db *DB) Rules(userID uint) ([]Rule, error) {
	var out []Rule
	err := db.Where("user_id = ?", userID).Order("id asc").Find(&out).Error
	return out, err
}

func (db *DB) Colors(userID uint) (map[string]string, error) {
	var rows []TagColor
	if err := db.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Tag] = r.Color
	}
	return m, nil
}

func (db *DB) IgnoredViolations(userID uint) ([]IgnoredViolation, error) {
	var out []IgnoredViolation
	err := db.Where("user_id = ?", userID).Order("ts asc").Find(&out).Error
	return out, err
}

// ── Ingest (idempotent; stamps UserID on every row) ──

// IngestViolations upserts report lines; a re-sent (user,ts,keyword,url) no-ops.
func (db *DB) IngestViolations(userID uint, rows []Violation) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].UserID = userID
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ts"}, {Name: "keyword"}, {Name: "url"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

// IngestUnblocks upserts unblock events; on conflict updates restore_ts+reason so
// a later sync can close an unblock that was open when first sent.
func (db *DB) IngestUnblocks(userID uint, rows []Unblock) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].UserID = userID
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ts"}, {Name: "domain"}},
		DoUpdates: clause.AssignmentColumns([]string{"restore_ts", "reason"}),
	}).CreateInBatches(rows, 500).Error
}

func (db *DB) IngestLifecycle(userID uint, rows []LifecycleEvent) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].UserID = userID
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ts"}, {Name: "type"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

func (db *DB) IngestUsage(userID uint, rows []UsageSample) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].UserID = userID
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ts"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

func (db *DB) IngestHeartbeats(userID uint, rows []Heartbeat) error {
	if len(rows) == 0 {
		return nil
	}
	for i := range rows {
		rows[i].UserID = userID
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "ts"}},
		DoNothing: true,
	}).CreateInBatches(rows, 500).Error
}

// stampUser sets UserID on every row via an accessor (used where a range loop
// would be noisier); kept generic-free for clarity.
func stampUser[T any](rows []T, field func(i int) *uint, userID uint) {
	for i := range rows {
		*field(i) = userID
	}
}

// ── Dashboard-local settings (replace-all, scoped to userID) ──

// SetRulesConfig replaces the account's rules + colours in one transaction.
func (db *DB) SetRulesConfig(userID uint, rules []Rule, colors map[string]string) error {
	return db.Transaction(func(tx *DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&Rule{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&TagColor{}).Error; err != nil {
			return err
		}
		if len(rules) > 0 {
			for i := range rules {
				rules[i].UserID = userID
			}
			if err := tx.Create(&rules).Error; err != nil {
				return err
			}
		}
		if len(colors) > 0 {
			tc := make([]TagColor, 0, len(colors))
			for tag, color := range colors {
				tc = append(tc, TagColor{UserID: userID, Tag: tag, Color: color})
			}
			if err := tx.Create(&tc).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// SetIgnored replaces the account's false-positive ignore list.
func (db *DB) SetIgnored(userID uint, rows []IgnoredViolation) error {
	return db.Transaction(func(tx *DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&IgnoredViolation{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			for i := range rows {
				rows[i].UserID = userID
			}
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

// Counts returns row counts per stats table for one account (health endpoint).
func (db *DB) Counts(userID uint) map[string]int64 {
	out := map[string]int64{}
	for name, m := range map[string]any{
		"violations": &Violation{}, "unblocks": &Unblock{},
		"lifecycle": &LifecycleEvent{}, "usage": &UsageSample{},
		"heartbeat": &Heartbeat{}, "rules": &Rule{}, "ignored": &IgnoredViolation{},
	} {
		var n int64
		db.Model(m).Where("user_id = ?", userID).Count(&n)
		out[name] = n
	}
	return out
}
