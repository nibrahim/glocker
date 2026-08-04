// Package stats serves the glockpeek dashboard (usage + exposure analysis) and
// its JSON API. It is served at the root ("/") by the standalone glockpeek
// process. The frontend assets are embedded in the binary, so nothing external
// is required at runtime; access is restricted to localhost since the page shows
// personal browsing/usage data.
package stats

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"

	"glocker/internal/reports"
)

// assets is a copy of glockpeek-web/public (the standalone node dev app). Keep
// the two in sync; the frontend is written path-agnostically so the same files
// serve at "/" (node) and "/stats/" (here).
//
//go:embed assets
var assetsFS embed.FS

// Default log/config locations for the daemon. Each is overridable via the same
// environment variables the standalone node app uses, which also makes testing
// easy.
const (
	DefaultUsageLogPath = "/var/log/glocker-usage.jsonl"
	// Rules are mutable state written by the dashboard, so they live under
	// /var/lib (app state), not /etc (static config).
	DefaultRulesPath = "/var/lib/glocker/usage-rules.json"
	// Ignored violations (false positives marked from the dashboard) are also
	// mutable dashboard state, kept alongside the rules.
	DefaultIgnoredPath = "/var/lib/glocker/ignored-violations.json"
)

// Options lets the caller (the daemon, from config) pin the usage log and rules
// file. Empty fields fall back to the matching env var, then the default.
type Options struct {
	UsageLog  string
	RulesFile string
}

var opts Options

// logPaths holds the resolved file locations for one request.
type logPaths struct {
	reports   string
	unblocks  string
	lifecycle string
	heartbeat string
	usage     string
	rules     string
	ignored   string
}

func resolvePaths() logPaths {
	return logPaths{
		reports:   envOr("GLOCKER_REPORTS_LOG", reports.DefaultReportsLogPath),
		unblocks:  envOr("GLOCKER_UNBLOCKS_LOG", reports.DefaultUnblocksLogPath),
		lifecycle: envOr("GLOCKER_LIFECYCLE_LOG", reports.DefaultLifecycleLogPath),
		heartbeat: envOr("GLOCKER_HEARTBEAT_LOG", reports.DefaultHeartbeatLogPath),
		usage:     pick(opts.UsageLog, "GLOCKER_USAGE_LOG", DefaultUsageLogPath),
		rules:     pick(opts.RulesFile, "GLOCKER_USAGE_RULES", DefaultRulesPath),
		ignored:   envOr("GLOCKER_IGNORED_VIOLATIONS", DefaultIgnoredPath),
	}
}

// pick prefers an explicit value, then an env var, then the default.
func pick(explicit, env, def string) string {
	if explicit != "" {
		return explicit
	}
	return envOr(env, def)
}

func envOr(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

// Register mounts the dashboard and its API onto mux, using o to locate the
// usage log and rules file (empty fields fall back to env/defaults). glockpeek
// owns the whole listener, so the dashboard is served at the root ("/"). The
// legacy "/stats/" prefix (from when this was mounted inside the daemon's web
// server) is redirected to "/" for old bookmarks. All routes are restricted to
// loopback clients.
func Register(mux *http.ServeMux, o Options) {
	opts = o
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
}

// handleIgnored serves the false-positive ignore list: GET returns it, PUT
// replaces it. The dashboard sends the full list on every change.
func handleIgnored(w http.ResponseWriter, r *http.Request) {
	path := resolvePaths().ignored
	switch r.Method {
	case http.MethodGet:
		list, err := loadIgnored(path)
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
		saved, err := saveIgnored(in.Ignored, path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ignored": saved})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleData(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, buildData(resolvePaths(), time.Now()))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	p := resolvePaths()
	data := buildData(p, time.Now())
	writeJSON(w, map[string]any{
		"ok":      true,
		"sources": data.Sources,
		"paths": map[string]string{
			"reports": p.reports, "unblocks": p.unblocks,
			"lifecycle": p.lifecycle, "heartbeat": p.heartbeat,
			"usage": p.usage, "rules": p.rules, "ignored": p.ignored,
		},
	})
}

func handleRules(w http.ResponseWriter, r *http.Request) {
	path := resolvePaths().rules
	switch r.Method {
	case http.MethodGet:
		cfg, err := loadConfig(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, cfg)
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 256*1024))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var in rulesConfig
		in.Rules, in.Colors = parseConfigBytes(body)
		saved, err := saveConfig(in, path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, saved)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
	http.Error(w, "forbidden: /stats is available on localhost only", http.StatusForbidden)
	return false
}
