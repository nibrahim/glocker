// Package store is glockpeek's persistence layer. It is dialect-agnostic via
// GORM: sqlite locally (pure-Go glebarez driver, cgo-free) and postgres for a
// hosted instance — switching is a driver + DSN change, no query rewrites.
//
// The models mirror the JSON shapes the dashboard already consumes
// (internal/stats data.go), so the sync/ingest path (glocker -> glockpeek) and
// the dashboard read path map onto them with minimal translation. Timestamps are
// epoch milliseconds, matching the rest of the stack.
//
// Records that the dashboard derives (unmanaged spans from lifecycle events,
// downtime spans from heartbeats) are NOT stored — they stay computed.
package store

// Violation is one content/URL-keyword report (from the reports log). The
// (TS, Keyword, URL) triple uniquely identifies a report line — the same key the
// ignore overlay uses — so ingest can upsert idempotently.
type Violation struct {
	ID      uint   `gorm:"primaryKey" json:"-"`
	TS      int64  `gorm:"index;uniqueIndex:ux_violation,priority:1" json:"ts"`
	Keyword string `gorm:"uniqueIndex:ux_violation,priority:2" json:"keyword"`
	URL     string `gorm:"uniqueIndex:ux_violation,priority:3" json:"url"`
	Type    string `json:"type"`
	Domain  string `json:"domain"`
}

// Unblock is one temporary-unblock event; RestoreTS is nil while still open.
type Unblock struct {
	ID        uint   `gorm:"primaryKey" json:"-"`
	TS        int64  `gorm:"index;uniqueIndex:ux_unblock,priority:1" json:"ts"`
	Domain    string `gorm:"uniqueIndex:ux_unblock,priority:2" json:"domain"`
	RestoreTS *int64 `json:"restoreTs"`
	Reason    string `json:"reason"`
}

// LifecycleEvent is an install/uninstall record (drives the UNMANAGED overlay).
type LifecycleEvent struct {
	ID     uint   `gorm:"primaryKey" json:"-"`
	TS     int64  `gorm:"index;uniqueIndex:ux_lifecycle,priority:1" json:"ts"`
	Type   string `gorm:"uniqueIndex:ux_lifecycle,priority:2" json:"type"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

// UsageSample is one focused-window sample from the usage tracker. One row per
// timestamp (the active window is flattened in; the full window list is dropped,
// same as the file-based reader).
type UsageSample struct {
	ID             uint   `gorm:"primaryKey" json:"-"`
	TS             int64  `gorm:"uniqueIndex" json:"ts"`
	IdleMS         int64  `json:"idleMs"`
	ActiveClass    string `json:"activeClass"`
	ActiveInstance string `json:"activeInstance"`
	ActiveTitle    string `json:"activeTitle"`
	WindowCount    int    `json:"windowCount"`
}

// Heartbeat is one liveness sample recorded by glockdoc (drives downtime spans).
type Heartbeat struct {
	ID    uint  `gorm:"primaryKey" json:"-"`
	TS    int64 `gorm:"uniqueIndex" json:"ts"`
	Alive bool  `json:"alive"`
}

// Rule is one usage-categorization rule (optional program/title regex -> tag).
// Dashboard-local state (created on glockpeek, not synced from glocker).
type Rule struct {
	ID      uint   `gorm:"primaryKey" json:"-"`
	Program string `json:"program"`
	Title   string `json:"title"`
	Tag     string `json:"tag"`
}

// TagColor maps a tag to its display colour (#rrggbb). Tag is the key.
type TagColor struct {
	Tag   string `gorm:"primaryKey" json:"tag"`
	Color string `json:"color"`
}

// IgnoredViolation marks a report line as a false positive so the dashboard
// stops counting it. Matched on (TS, Keyword, URL), same identity as Violation.
type IgnoredViolation struct {
	ID      uint   `gorm:"primaryKey" json:"-"`
	TS      int64  `gorm:"uniqueIndex:ux_ignored,priority:1" json:"ts"`
	Keyword string `gorm:"uniqueIndex:ux_ignored,priority:2" json:"keyword"`
	URL     string `gorm:"uniqueIndex:ux_ignored,priority:3" json:"url"`
	Domain  string `json:"domain"`
}

// AllModels is the migration set, in dependency order (none depend on each other
// yet). Kept in one place so Migrate and tests stay in sync.
func AllModels() []any {
	return []any{
		&Violation{},
		&Unblock{},
		&LifecycleEvent{},
		&UsageSample{},
		&Heartbeat{},
		&Rule{},
		&TagColor{},
		&IgnoredViolation{},
	}
}
