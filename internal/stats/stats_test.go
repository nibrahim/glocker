package stats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMux registers the stats routes on a fresh mux (http.DefaultServeMux can
// only be registered once per process).
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	Register(mux)
	return mux
}

func local(req *http.Request) *http.Request {
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}

// ── rules store ─────────────────────────────────────────
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

func TestLoadSaveConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-rules.json")
	saved, err := saveConfig(rulesConfig{
		Rules:  []Rule{{Program: "firefox", Title: "YouTube", Tag: "Activity:leisure"}},
		Colors: map[string]string{"Activity:leisure": "#c0468f", "bad": "x"},
	}, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Colors) != 1 {
		t.Errorf("bad colour not dropped: %+v", saved.Colors)
	}
	got, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 1 || got.Rules[0].Tag != "Activity:leisure" || got.Colors["Activity:leisure"] != "#c0468f" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestLoadConfigLegacyArrayAndMissing(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacy, []byte(`[{"program":"","title":"","tag":"A:b"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _ := loadConfig(legacy)
	if len(cfg.Rules) != 1 || cfg.Rules[0].Tag != "A:b" || cfg.Colors == nil {
		t.Fatalf("legacy array not handled: %+v", cfg)
	}
	// Missing file -> empty, non-nil, no error.
	cfg, err := loadConfig(filepath.Join(dir, "nope.json"))
	if err != nil || len(cfg.Rules) != 0 || cfg.Colors == nil {
		t.Fatalf("missing file: cfg=%+v err=%v", cfg, err)
	}
}

// ── usage reader ────────────────────────────────────────
func TestReadUsageLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	lines := strings.Join([]string{
		`{"ts":"2026-07-07T15:36:11.24+05:30","idle_ms":5,"windows":[{"active":true,"class":"kitty","instance":"kitty","title":"zsh"},{"active":false,"class":"Emacs","title":"x"}]}`,
		`garbage`,
		`{"ts":"2026-07-07T15:36:01+05:30","idle_ms":90000,"windows":[]}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	samples, ok, err := readUsageLog(path)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2", len(samples))
	}
	// sorted ascending -> the 15:36:01 empty-window sample first
	if samples[0].Active != nil || samples[0].WindowCount != 0 || samples[0].IdleMS != 90000 {
		t.Errorf("sample[0] = %+v", samples[0])
	}
	if samples[1].Active == nil || samples[1].Active.Class != "kitty" || samples[1].WindowCount != 2 {
		t.Errorf("sample[1] = %+v (active=%+v)", samples[1], samples[1].Active)
	}

	// Missing file -> ok=false, no error.
	_, ok, err = readUsageLog(filepath.Join(dir, "none.jsonl"))
	if ok || err != nil {
		t.Errorf("missing usage log: ok=%v err=%v", ok, err)
	}
}

// ── handlers ────────────────────────────────────────────
func TestLocalhostGuard(t *testing.T) {
	mux := newMux()
	// Default httptest RemoteAddr (192.0.2.1) is non-loopback -> 403.
	for _, p := range []string{"/stats/", "/stats/api/data", "/stats/api/rules"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s from non-loopback: got %d, want 403", p, rec.Code)
		}
	}
}

func TestServesIndexAndAssets(t *testing.T) {
	mux := newMux()

	// /stats redirects to /stats/
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/stats", nil)))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/stats/" {
		t.Errorf("/stats redirect: code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	// index + assets
	for _, tc := range []struct{ path, needle string }{
		{"/stats/", "glock"},
		{"/stats/app.js", "renderUsage"},
		{"/stats/styles.css", "--bg"},
		{"/stats/chart.umd.min.js", "Chart"},
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

func TestDataEndpoint(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Setenv("GLOCKER_REPORTS_LOG", write("reports.log", "[2025-11-17 15:35:46] | url-keyword:porn | https://www.google.com/search?q=porn\n"))
	t.Setenv("GLOCKER_LIFECYCLE_LOG", write("lifecycle.log", strings.Join([]string{
		`{"timestamp":"2026-02-11T14:00:00+05:30","type":"uninstall","reason":"temp"}`,
		`{"timestamp":"2026-02-12T14:00:00+05:30","type":"install"}`,
	}, "\n")))
	t.Setenv("GLOCKER_USAGE_LOG", write("usage.jsonl",
		`{"ts":"2026-07-07T15:36:11+05:30","idle_ms":5,"windows":[{"active":true,"class":"kitty","title":"zsh"}]}`+"\n"))
	// Point unblocks at a missing path so sources.unblocks is false (don't fall
	// back to the machine's real /var/log default).
	t.Setenv("GLOCKER_UNBLOCKS_LOG", filepath.Join(dir, "absent-unblocks.log"))

	mux := newMux()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/stats/api/data", nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("data: got %d", rec.Code)
	}
	var d dataResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if !d.Sources.Reports || !d.Sources.Lifecycle || !d.Sources.Usage || d.Sources.Unblocks {
		t.Errorf("sources = %+v", d.Sources)
	}
	if len(d.Violations) != 1 || d.Violations[0].Keyword != "porn" || d.Violations[0].Domain != "www.google.com" {
		t.Errorf("violations = %+v", d.Violations)
	}
	if len(d.Unmanaged) != 1 || d.Unmanaged[0].Open || d.Unmanaged[0].Reason != "temp" {
		t.Errorf("unmanaged = %+v", d.Unmanaged)
	}
	if len(d.Usage) != 1 || d.Usage[0].Active == nil || d.Usage[0].Active.Class != "kitty" {
		t.Errorf("usage = %+v", d.Usage)
	}
	// Arrays must never serialize as null.
	if !strings.Contains(rec.Body.String(), `"unblocks":[]`) {
		t.Errorf("empty unblocks should be [] not null: %s", rec.Body.String())
	}
}

func TestRulesGetPut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-rules.json")
	t.Setenv("GLOCKER_USAGE_RULES", path)
	mux := newMux()

	// GET on empty -> {rules:[],colors:{}}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/stats/api/rules", nil)))
	if !strings.Contains(rec.Body.String(), `"rules":[]`) || !strings.Contains(rec.Body.String(), `"colors":{}`) {
		t.Fatalf("empty GET = %s", rec.Body.String())
	}

	// PUT rules + colours (with a junk rule and bad colour)
	body := `{"rules":[{"program":"^Emacs$","title":"","tag":"Activity:work"},{"nope":true}],"colors":{"Activity:work":"#3aa0ff","x":"bad"}}`
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("PUT", "/stats/api/rules", strings.NewReader(body))))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var cfg rulesConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Tag != "Activity:work" || len(cfg.Colors) != 1 || cfg.Colors["Activity:work"] != "#3aa0ff" {
		t.Fatalf("PUT result = %+v", cfg)
	}

	// It persisted to disk and GET returns it.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, local(httptest.NewRequest("GET", "/stats/api/rules", nil)))
	if !strings.Contains(rec.Body.String(), "Activity:work") || !strings.Contains(rec.Body.String(), "#3aa0ff") {
		t.Errorf("GET after PUT = %s", rec.Body.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("rules file not written: %v", err)
	}
}
