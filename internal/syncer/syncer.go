// Package syncer ships glocker's local /var records to a glockpeek instance
// (local or remote) via its ingest API. It is the agent side of the local-first
// design: glocker keeps recording to files as the source of truth and enforcing
// offline; the syncer just mirrors new records up on a timer (a one-shot
// backfill at startup, then incremental). It is deliberately decoupled from the
// DB layer — it posts plain JSON, so the agent binary carries no GORM/driver
// weight.
//
// Idempotency: glockpeek upserts on each record's natural key, so re-sending
// overlapping batches (after a restart or a failed POST) never loses or
// double-counts. Failures are logged and retried on the next tick; nothing here
// blocks enforcement.
package syncer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"glocker/internal/config"
	"glocker/internal/reports"
	"glocker/internal/state"
	"glocker/internal/usage"
)

// Payload mirrors glockpeek's ingest shape (store models' JSON tags). Defined
// here rather than imported so the agent stays free of the DB layer.
type Payload struct {
	Violations []violation `json:"violations,omitempty"`
	Unblocks   []unblock   `json:"unblocks,omitempty"`
	Lifecycle  []lifecycle `json:"lifecycle,omitempty"`
	Usage      []usageRow  `json:"usage,omitempty"`
	Heartbeat  []heartbeat `json:"heartbeat,omitempty"`
}

type violation struct {
	TS      int64  `json:"ts"`
	Type    string `json:"type"`
	Keyword string `json:"keyword"`
	URL     string `json:"url"`
	Domain  string `json:"domain"`
}
type unblock struct {
	TS        int64  `json:"ts"`
	Domain    string `json:"domain"`
	RestoreTS *int64 `json:"restoreTs"`
	Reason    string `json:"reason"`
}
type lifecycle struct {
	TS     int64  `json:"ts"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}
type usageRow struct {
	TS             int64  `json:"ts"`
	IdleMS         int64  `json:"idleMs"`
	ActiveClass    string `json:"activeClass"`
	ActiveInstance string `json:"activeInstance"`
	ActiveTitle    string `json:"activeTitle"`
	WindowCount    int    `json:"windowCount"`
}
type heartbeat struct {
	TS    int64 `json:"ts"`
	Alive bool  `json:"alive"`
}

// Syncer holds resolved config for one agent->glockpeek link.
type Syncer struct {
	url      string // base, e.g. http://127.0.0.1:4317
	token    string
	interval time.Duration
	paths    struct{ reports, unblocks, lifecycle, heartbeat, usage string }
	client   *http.Client

	// cursors[source] is the max TS already accepted for that source; each cycle
	// sends records with TS >= cursor (the boundary re-send is idempotent).
	cursors map[string]int64
}

// New builds a Syncer from config, applying defaults.
func New(cfg *config.Config) *Syncer {
	s := &Syncer{
		url:      strings.TrimRight(orDefault(cfg.Sync.GlockpeekURL, config.DefaultGlockpeekURL), "/"),
		token:    cfg.Sync.Token,
		interval: time.Duration(orInt(cfg.Sync.IntervalSeconds, config.DefaultSyncIntervalSeconds)) * time.Second,
		client:   &http.Client{Timeout: 30 * time.Second},
		cursors:  map[string]int64{},
	}
	s.paths.reports = reports.DefaultReportsLogPath
	s.paths.unblocks = reports.DefaultUnblocksLogPath
	s.paths.lifecycle = orDefault(cfg.Lifecycle.LogFile, reports.DefaultLifecycleLogPath)
	s.paths.heartbeat = reports.DefaultHeartbeatLogPath
	s.paths.usage = orDefault(cfg.UsageMonitor.LogFile, config.DefaultUsageLogFile)
	return s
}

// Run seeds cursors from glockpeek's high-water marks, does the initial sync,
// then syncs on the interval. Blocks; run in a goroutine. Every error is logged
// and retried next tick — enforcement is never affected.
func (s *Syncer) Run() {
	slog.Info("syncer starting", "target", s.url, "interval", s.interval)
	s.seedCursors() // ask glockpeek what it already has, so we only send the gap
	s.syncOnce()
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for range t.C {
		s.syncOnce()
	}
}

// seedCursors asks glockpeek for the max TS it holds per source and seeds the
// cursors from that, so a restart doesn't re-send everything. On any error
// (glockpeek down, etc.) cursors stay at 0 and the next sync does a full
// backfill — always safe because ingest is idempotent.
func (s *Syncer) seedCursors() {
	req, err := http.NewRequest(http.MethodGet, s.url+"/api/ingest", nil)
	if err != nil {
		return
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("syncer: couldn't read glockpeek cursors; will backfill", "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("syncer: cursor request rejected; will backfill", "status", resp.Status)
		return
	}
	var body struct {
		Cursors map[string]int64 `json:"cursors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return
	}
	for src, ts := range body.Cursors {
		s.cursors[src] = ts
	}
	slog.Info("syncer: seeded cursors from glockpeek", "cursors", s.cursors)
}

// syncOnce builds a payload of records newer than the cursors and posts it.
// Cursors only advance on a successful post, so a failed cycle is retried whole.
func (s *Syncer) syncOnce() {
	p, maxTS := s.build()
	if p.empty() {
		return
	}
	if err := s.post(p); err != nil {
		slog.Warn("syncer post failed; will retry", "error", err)
		return
	}
	for src, ts := range maxTS {
		if ts > s.cursors[src] {
			s.cursors[src] = ts
		}
	}
	counts := map[string]int{
		"violations": len(p.Violations), "unblocks": len(p.Unblocks),
		"lifecycle": len(p.Lifecycle), "usage": len(p.Usage), "heartbeat": len(p.Heartbeat),
	}
	state.RecordSync(counts) // surfaced by `glocker -status`
	slog.Debug("syncer pushed batch", "counts", counts)
}

// build assembles the payload from the logs, filtered to TS >= each cursor, and
// returns the max TS seen per source (for advancing cursors after a good post).
func (s *Syncer) build() (Payload, map[string]int64) {
	var p Payload
	max := map[string]int64{}

	if rows, err := reports.ParseReportsLog(s.paths.reports); err == nil {
		cur := s.cursors["reports"]
		for _, e := range rows {
			ts := e.Timestamp.UnixMilli()
			if ts < cur {
				continue
			}
			p.Violations = append(p.Violations, violation{
				TS: ts, Type: string(e.Type), Keyword: e.Keyword, URL: e.URL, Domain: e.Domain,
			})
			max["reports"] = maxi(max["reports"], ts)
		}
	}

	if rows, err := reports.ParseUnblocksLog(s.paths.unblocks); err == nil {
		cur := s.cursors["unblocks"]
		for _, e := range rows {
			if e.UnblockTime.IsZero() {
				continue
			}
			ts := e.UnblockTime.UnixMilli()
			if ts < cur {
				continue
			}
			u := unblock{TS: ts, Domain: e.Domain, Reason: e.Reason}
			if !e.RestoreTime.IsZero() {
				r := e.RestoreTime.UnixMilli()
				u.RestoreTS = &r
			}
			p.Unblocks = append(p.Unblocks, u)
			max["unblocks"] = maxi(max["unblocks"], ts)
		}
	}

	if rows, err := reports.ParseLifecycleLog(s.paths.lifecycle); err == nil {
		cur := s.cursors["lifecycle"]
		for _, e := range rows {
			ts := e.Timestamp.UnixMilli()
			if ts < cur {
				continue
			}
			p.Lifecycle = append(p.Lifecycle, lifecycle{TS: ts, Type: e.Type, Reason: e.Reason, Note: e.Note})
			max["lifecycle"] = maxi(max["lifecycle"], ts)
		}
	}

	if rows, err := reports.ParseHeartbeatLog(s.paths.heartbeat); err == nil {
		cur := s.cursors["heartbeat"]
		for _, e := range rows {
			ts := e.Timestamp.UnixMilli()
			if ts < cur {
				continue
			}
			p.Heartbeat = append(p.Heartbeat, heartbeat{TS: ts, Alive: e.Alive})
			max["heartbeat"] = maxi(max["heartbeat"], ts)
		}
	}

	if rows, err := parseUsageLog(s.paths.usage); err == nil {
		cur := s.cursors["usage"]
		for _, r := range rows {
			if r.TS < cur {
				continue
			}
			p.Usage = append(p.Usage, r)
			max["usage"] = maxi(max["usage"], r.TS)
		}
	}

	return p, max
}

func (s *Syncer) post(p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, s.url+"/api/ingest", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest returned %s", resp.Status)
	}
	return nil
}

// parseUsageLog reads the usage JSONL log into ingest rows (flattening the active
// window), tolerating missing files and malformed lines.
func parseUsageLog(path string) ([]usageRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []usageRow
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var smp usage.Sample
		if err := json.Unmarshal([]byte(line), &smp); err != nil {
			continue
		}
		row := usageRow{TS: smp.Timestamp.UnixMilli(), IdleMS: smp.IdleMS, WindowCount: len(smp.Windows)}
		for _, w := range smp.Windows {
			if w.Active {
				row.ActiveClass, row.ActiveInstance, row.ActiveTitle = w.Class, w.Instance, w.Title
				break
			}
		}
		out = append(out, row)
	}
	return out, sc.Err()
}

func (p Payload) empty() bool {
	return len(p.Violations)+len(p.Unblocks)+len(p.Lifecycle)+len(p.Usage)+len(p.Heartbeat) == 0
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
func maxi(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
