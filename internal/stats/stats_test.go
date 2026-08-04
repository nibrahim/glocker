package stats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"glocker/internal/store"
)

// newMux registers the stats routes on a fresh mux backed by a temp-file sqlite
// store, and returns both.
func newMux(t *testing.T) (*http.ServeMux, *store.DB) {
	t.Helper()
	db, err := store.Open(store.Options{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mux := http.NewServeMux()
	Register(mux, db)
	return mux, db
}

func local(req *http.Request) *http.Request {
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}

// ── rules sanitation (pure) ─────────────────────────────
func TestSanitizeRulesAndColors(t *testing.T) {
	rules := sanitizeRules([]Rule{
		{Program: " ^Emacs$ ", Title: "glocker", Tag: " Project:glocker "},
		{Tag: ""}, // no tag -> dropped
	})
	if len(rules) != 1 || rules[0].Program != "^Emacs$" || rules[0].Tag != "Project:glocker" {
		t.Fatalf("sanitizeRules = %+v", rules)
	}
	colors := sanitizeColors(map[string]string{
		"Activity:work": "#F0A020", // normalized to lower
		"Bad:short":     "#fff",    // dropped
		"":              "#000000", // empty key dropped
	})
	if len(colors) != 1 || colors["Activity:work"] != "#f0a020" {
		t.Fatalf("sanitizeColors = %+v", colors)
	}
}

func TestParseConfigLegacyArray(t *testing.T) {
	rules, colors := parseConfigBytes([]byte(`[{"program":"","title":"","tag":"A:b"}]`))
	if len(rules) != 1 || rules[0].Tag != "A:b" || colors == nil {
		t.Fatalf("legacy array not handled: %+v / %+v", rules, colors)
	}
}

// ── handlers ────────────────────────────────────────────
func TestLocalhostGuard(t *testing.T) {
	mux, _ := newMux(t)
	// Default httptest RemoteAddr (192.0.2.1) is non-loopback -> 403.
	for _, p := range []string{"/", "/api/data", "/api/rules", "/api/ingest"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from non-loopback: got %d, want 403", p, rec.Code)
		}
	}
}

func TestServesIndexAndAssets(t *testing.T) {
	mux, _ := newMux(t)

	// Legacy /stats and /stats/ redirect to /
	for _, p := range []string{"/stats", "/stats/"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, local(httptest.NewRequest("GET", p, nil)))
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/" {
			t.Errorf("%s redirect: code=%d loc=%q", p, rec.Code, rec.Header().Get("Location"))
		}
	}

	for _, tc := range []struct{ path, needle string }{
		{"/", "glock"},
		{"/app.js", "renderUsage"},
		{"/styles.css", "--bg"},
		{"/chart.umd.min.js", "Chart"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, local(httptest.NewRequest("GET", tc.path, nil)))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: got %d, want 200", tc.path, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), tc.needle) {
			t.Errorf("%s: body missing %q", tc.path, tc.needle)
		}
	}
}

// TestIngestThenData drives the full server path: POST a batch to /api/ingest,
// then GET /api/data and check the records come back (with derived unmanaged),
// and that re-ingesting is idempotent.
func TestIngestThenData(t *testing.T) {
	mux, _ := newMux(t)

	payload := `{
      "violations":[{"ts":1000,"type":"url-keyword","keyword":"porn","url":"https://x/?q=porn","domain":"x"}],
      "lifecycle":[
        {"ts":100000,"type":"uninstall","reason":"temp"},
        {"ts":100000000,"type":"install"}
      ],
      "usage":[{"ts":500,"idleMs":5,"activeClass":"kitty","windowCount":1}],
      "heartbeat":[{"ts":600,"alive":true}]
    }`

	ingest := func() {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, local(httptest.NewRequest("POST", "/api/ingest", strings.NewReader(payload))))
		if rec.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
		}
	}
	ingest()
	ingest() // idempotent: second batch must not duplicate

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/api/data", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("data: %d", rec.Code)
	}
	var d dataResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if !d.Sources.Reports || !d.Sources.Lifecycle || !d.Sources.Usage || !d.Sources.Heartbeat {
		t.Errorf("sources = %+v", d.Sources)
	}
	if len(d.Violations) != 1 || d.Violations[0].Keyword != "porn" {
		t.Errorf("violations = %+v", d.Violations)
	}
	if len(d.Usage) != 1 || d.Usage[0].Active == nil || d.Usage[0].Active.Class != "kitty" {
		t.Errorf("usage = %+v", d.Usage)
	}
	// uninstall(100000) -> install(100000000) is a >2min gap -> one unmanaged span.
	if len(d.Unmanaged) != 1 || d.Unmanaged[0].Reason != "temp" {
		t.Errorf("unmanaged = %+v", d.Unmanaged)
	}
	// Empty slices must serialize as [] not null.
	if !strings.Contains(rec.Body.String(), `"unblocks":[]`) {
		t.Errorf("empty unblocks should be [] not null: %s", rec.Body.String())
	}
}

// TestIgnoredHidesViolation marks the ingested violation as a false positive and
// checks it disappears from /api/data.
func TestIgnoredHidesViolation(t *testing.T) {
	mux, _ := newMux(t)
	mux.ServeHTTP(httptest.NewRecorder(), local(httptest.NewRequest("POST", "/api/ingest",
		strings.NewReader(`{"violations":[{"ts":1000,"keyword":"porn","url":"https://x/?q=porn","domain":"x"}]}`))))

	put := `{"ignored":[{"ts":1000,"keyword":"porn","url":"https://x/?q=porn","domain":"x"}]}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("PUT", "/api/ignored", strings.NewReader(put))))
	if rec.Code != http.StatusOK {
		t.Fatalf("put ignored: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/api/data", nil)))
	var d dataResponse
	json.Unmarshal(rec.Body.Bytes(), &d)
	if len(d.Violations) != 0 {
		t.Errorf("ignored violation should be hidden, got %+v", d.Violations)
	}
}

func TestRulesGetPut(t *testing.T) {
	mux, _ := newMux(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/api/rules", nil)))
	if !strings.Contains(rec.Body.String(), `"rules":[]`) || !strings.Contains(rec.Body.String(), `"colors":{}`) {
		t.Fatalf("empty GET = %s", rec.Body.String())
	}

	body := `{"rules":[{"program":"^Emacs$","title":"","tag":"Activity:work"},{"nope":true}],"colors":{"Activity:work":"#3aa0ff","x":"bad"}}`
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("PUT", "/api/rules", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/api/rules", nil)))
	var cfg rulesConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Tag != "Activity:work" || cfg.Colors["Activity:work"] != "#3aa0ff" {
		t.Fatalf("persisted rules = %+v", cfg)
	}
	if len(cfg.Colors) != 1 {
		t.Errorf("bad colour should have been dropped: %+v", cfg.Colors)
	}
}
