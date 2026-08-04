// Package stats serves the glockpeek dashboard (usage + exposure analysis) and
// its JSON API. It is served at the root ("/") by the standalone glockpeek
// process, reading from glockpeek's DB (populated by the glocker syncer via the
// ingest API). The frontend assets are embedded in the binary, so nothing
// external is required at runtime; access is restricted to localhost since the
// page shows personal browsing/usage data.
package stats

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"time"

	"glocker/internal/store"
)

// assets is a copy of glockpeek-web/public (the standalone node dev app). Keep
// the two in sync; the frontend is written path-agnostically so the same files
// serve at "/" (node) and "/" (here).
//
//go:embed assets
var assetsFS embed.FS

// db is the store the handlers read/write. Set by Register.
var db *store.DB

// Register mounts the dashboard and its API onto mux, backed by database.
// glockpeek owns the whole listener, so the dashboard is served at the root
// ("/"). The legacy "/stats/" prefix (from when this was mounted inside the
// daemon's web server) 301-redirects to "/". All routes are loopback-only.
func Register(mux *http.ServeMux, database *store.DB) {
	db = database
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return // embed failure is a build-time bug; nothing to serve
	}
	fileServer := http.FileServer(http.FS(sub))

	// Legacy /stats and /stats/* -> / (dashboard moved to the root).
	redirectToRoot := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	}
	mux.Handle("/stats", localGuard(http.HandlerFunc(redirectToRoot)))
	mux.Handle("/stats/", localGuard(http.HandlerFunc(redirectToRoot)))

	// Dashboard + API at the root.
	mux.Handle("/", localGuard(fileServer))
	mux.Handle("/api/data", localGuard(http.HandlerFunc(handleData)))
	mux.Handle("/api/health", localGuard(http.HandlerFunc(handleHealth)))
	mux.Handle("/api/rules", localGuard(http.HandlerFunc(handleRules)))
	mux.Handle("/api/ignored", localGuard(http.HandlerFunc(handleIgnored)))
	mux.Handle("/api/ingest", localGuard(http.HandlerFunc(handleIngest)))
}

func handleData(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, buildData(db, time.Now()))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "counts": db.Counts()})
}

// ingestPayload is the batch the glocker syncer POSTs. Every field is optional
// so the syncer can send only what changed (or everything, on the one-shot
// backfill). Ingest is idempotent, so overlapping batches are safe.
type ingestPayload struct {
	Violations []store.Violation      `json:"violations"`
	Unblocks   []store.Unblock        `json:"unblocks"`
	Lifecycle  []store.LifecycleEvent `json:"lifecycle"`
	Usage      []store.UsageSample    `json:"usage"`
	Heartbeat  []store.Heartbeat      `json:"heartbeat"`
}

// handleIngest accepts a batch of records from the syncer and upserts them.
func handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024*1024))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in ingestPayload
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, step := range []func() error{
		func() error { return db.IngestViolations(in.Violations) },
		func() error { return db.IngestUnblocks(in.Unblocks) },
		func() error { return db.IngestLifecycle(in.Lifecycle) },
		func() error { return db.IngestUsage(in.Usage) },
		func() error { return db.IngestHeartbeats(in.Heartbeat) },
	} {
		if err := step(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]any{
		"accepted": map[string]int{
			"violations": len(in.Violations), "unblocks": len(in.Unblocks),
			"lifecycle": len(in.Lifecycle), "usage": len(in.Usage),
			"heartbeat": len(in.Heartbeat),
		},
	})
}

// handleRules serves the usage categorization config: GET returns it, PUT
// replaces it. The dashboard sends the full {rules, colors} on every change.
func handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules, err := db.Rules()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		colors, err := db.Colors()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, rulesConfig{Rules: toStatsRules(rules), Colors: colors})
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rules, colors := parseConfigBytes(body)
		if err := db.SetRulesConfig(toStoreRules(rules), colors); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, rulesConfig{Rules: rules, Colors: colors})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleIgnored serves the false-positive ignore list: GET returns it, PUT
// replaces it.
func handleIgnored(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := loadIgnored(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ignored": list})
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 512*1024))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var in struct {
			Ignored []IgnoredEntry `json:"ignored"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		saved, err := saveIgnored(in.Ignored, db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ignored": saved})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// toStatsRules / toStoreRules convert between the API rule shape (rules.go) and
// the persisted store.Rule.
func toStatsRules(in []store.Rule) []Rule {
	out := make([]Rule, 0, len(in))
	for _, r := range in {
		out = append(out, Rule{Program: r.Program, Title: r.Title, Tag: r.Tag})
	}
	return out
}

func toStoreRules(in []Rule) []store.Rule {
	out := make([]store.Rule, 0, len(in))
	for _, r := range in {
		out = append(out, store.Rule{Program: r.Program, Title: r.Title, Tag: r.Tag})
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	buf, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(bytes.TrimRight(buf, "\n"))
}

// localGuard wraps a handler so only loopback clients reach it.
func localGuard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(w, r) {
			return
		}
		h.ServeHTTP(w, r)
	})
}

// isLocal reports whether the request came from loopback, writing a 403 if not.
func isLocal(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	http.Error(w, "forbidden: the dashboard is available on localhost only", http.StatusForbidden)
	return false
}
