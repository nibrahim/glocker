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
	sdb, err := store.Open(store.Options{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = sdb.Close() })
	mux := http.NewServeMux()
	Register(mux, sdb, Options{Auth: true}) // most tests exercise the auth-on path
	return mux, sdb
}

// account creates a user and returns its id, a session cookie, and an API token.
func account(t *testing.T, sdb *store.DB, name string) (uint, *http.Cookie, string) {
	t.Helper()
	u, err := sdb.CreateUser(name, "pw-"+name)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	tok, err := sdb.CreateSession(u.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	api, err := sdb.CreateAPIToken(u.ID, "test")
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return u.ID, &http.Cookie{Name: sessionCookie, Value: tok}, api
}

func do(mux *http.ServeMux, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// ── auth gating ─────────────────────────────────────────
func TestAuthRequired(t *testing.T) {
	mux, _ := newMux(t)
	// No session cookie -> 401 on gated routes.
	for _, p := range []string{"/api/data", "/api/rules", "/api/ignored", "/api/me", "/api/health"} {
		if rec := do(mux, httptest.NewRequest("GET", p, nil)); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without session: got %d, want 401", p, rec.Code)
		}
	}
	// No bearer token -> 401 on ingest.
	if rec := do(mux, httptest.NewRequest("POST", "/api/ingest", strings.NewReader("{}"))); rec.Code != http.StatusUnauthorized {
		t.Errorf("ingest without token: got %d, want 401", rec.Code)
	}
	// Static assets are public.
	if rec := do(mux, httptest.NewRequest("GET", "/", nil)); rec.Code != http.StatusOK {
		t.Errorf("index should be public: got %d", rec.Code)
	}
}

// TestAuthDisabled is the self-hosted default: no login, no token — every route
// works and resolves to the single implicit account.
func TestAuthDisabled(t *testing.T) {
	sdb, err := store.Open(store.Options{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sdb.Close() })
	du, err := sdb.EnsureDefaultUser()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Register(mux, sdb, Options{Auth: false, DefaultUserID: du.ID})

	// Ingest with no token works and attributes to the default account.
	ing := httptest.NewRequest("POST", "/api/ingest",
		strings.NewReader(`{"violations":[{"ts":1,"keyword":"k","url":"u","domain":"d"}]}`))
	if rec := do(mux, ing); rec.Code != http.StatusOK {
		t.Fatalf("ingest without token (auth off): %d %s", rec.Code, rec.Body.String())
	}

	// Data with no cookie works.
	rec := do(mux, httptest.NewRequest("GET", "/api/data", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("data without cookie (auth off): %d", rec.Code)
	}
	var d dataResponse
	json.Unmarshal(rec.Body.Bytes(), &d)
	if len(d.Violations) != 1 {
		t.Errorf("want 1 violation for the implicit account, got %+v", d.Violations)
	}

	// /api/me reports auth:false so the frontend hides sign-out.
	me := do(mux, httptest.NewRequest("GET", "/api/me", nil))
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"auth":false`) {
		t.Errorf("/api/me (auth off): %d %s", me.Code, me.Body.String())
	}
}

func TestLoginFlow(t *testing.T) {
	mux, sdb := newMux(t)
	if _, err := sdb.CreateUser("noufal", "hunter2hunter2"); err != nil {
		t.Fatal(err)
	}

	// Bad password -> 401.
	bad := do(mux, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"noufal","password":"nope"}`)))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad login: got %d", bad.Code)
	}

	// Good password -> 200 + session cookie.
	ok := do(mux, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"noufal","password":"hunter2hunter2"}`)))
	if ok.Code != http.StatusOK {
		t.Fatalf("good login: got %d %s", ok.Code, ok.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range ok.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("login did not set a session cookie")
	}

	// The cookie authenticates /api/me.
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	if rec := do(mux, req); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "noufal") {
		t.Errorf("/api/me with cookie: %d %s", rec.Code, rec.Body.String())
	}
}

func TestServesIndexAndAssets(t *testing.T) {
	mux, _ := newMux(t)
	for _, p := range []string{"/stats", "/stats/"} {
		rec := do(mux, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/" {
			t.Errorf("%s redirect: code=%d loc=%q", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, tc := range []struct{ path, needle string }{
		{"/", "glock"},
		{"/app.js", "renderUsage"},
	} {
		rec := do(mux, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tc.needle) {
			t.Errorf("%s: got %d, missing %q", tc.path, rec.Code, tc.needle)
		}
	}
}

// TestIngestThenData drives the full authed path: POST a batch with a bearer
// token, then GET /api/data with a session cookie.
func TestIngestThenData(t *testing.T) {
	mux, sdb := newMux(t)
	_, cookie, api := account(t, sdb, "noufal")

	payload := `{
      "violations":[{"ts":1000,"type":"url-keyword","keyword":"porn","url":"https://x/?q=porn","domain":"x"}],
      "lifecycle":[{"ts":100000,"type":"uninstall","reason":"temp"},{"ts":100000000,"type":"install"}],
      "usage":[{"ts":500,"idleMs":5,"activeClass":"kitty","windowCount":1}],
      "heartbeat":[{"ts":600,"alive":true}]
    }`
	ingest := func() {
		req := httptest.NewRequest("POST", "/api/ingest", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+api)
		if rec := do(mux, req); rec.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
		}
	}
	ingest()
	ingest() // idempotent

	req := httptest.NewRequest("GET", "/api/data", nil)
	req.AddCookie(cookie)
	rec := do(mux, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("data: %d", rec.Code)
	}
	var d dataResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Violations) != 1 || d.Violations[0].Keyword != "porn" {
		t.Errorf("violations = %+v", d.Violations)
	}
	if len(d.Usage) != 1 || d.Usage[0].Active == nil || d.Usage[0].Active.Class != "kitty" {
		t.Errorf("usage = %+v", d.Usage)
	}
	if len(d.Unmanaged) != 1 || d.Unmanaged[0].Reason != "temp" {
		t.Errorf("unmanaged = %+v", d.Unmanaged)
	}
	if !strings.Contains(rec.Body.String(), `"unblocks":[]`) {
		t.Errorf("empty unblocks should be [] not null")
	}
}

// TestTenantSeparationOverHTTP proves account B's token can't surface account A's
// data through the API.
func TestTenantSeparationOverHTTP(t *testing.T) {
	mux, sdb := newMux(t)
	_, _, aliceTok := account(t, sdb, "alice")
	_, bobCookie, _ := account(t, sdb, "bob")

	req := httptest.NewRequest("POST", "/api/ingest",
		strings.NewReader(`{"violations":[{"ts":1,"keyword":"k","url":"u","domain":"d"}]}`))
	req.Header.Set("Authorization", "Bearer "+aliceTok)
	if rec := do(mux, req); rec.Code != http.StatusOK {
		t.Fatalf("alice ingest: %d", rec.Code)
	}

	// Bob reads his own data — must be empty.
	req = httptest.NewRequest("GET", "/api/data", nil)
	req.AddCookie(bobCookie)
	rec := do(mux, req)
	var d dataResponse
	json.Unmarshal(rec.Body.Bytes(), &d)
	if len(d.Violations) != 0 {
		t.Errorf("bob should not see alice's data, got %+v", d.Violations)
	}
}

func TestIgnoredHidesViolation(t *testing.T) {
	mux, sdb := newMux(t)
	_, cookie, api := account(t, sdb, "noufal")

	ing := httptest.NewRequest("POST", "/api/ingest",
		strings.NewReader(`{"violations":[{"ts":1000,"keyword":"porn","url":"https://x/?q=porn","domain":"x"}]}`))
	ing.Header.Set("Authorization", "Bearer "+api)
	do(mux, ing)

	put := httptest.NewRequest("PUT", "/api/ignored",
		strings.NewReader(`{"ignored":[{"ts":1000,"keyword":"porn","url":"https://x/?q=porn","domain":"x"}]}`))
	put.AddCookie(cookie)
	if rec := do(mux, put); rec.Code != http.StatusOK {
		t.Fatalf("put ignored: %d %s", rec.Code, rec.Body.String())
	}

	get := httptest.NewRequest("GET", "/api/data", nil)
	get.AddCookie(cookie)
	var d dataResponse
	json.Unmarshal(do(mux, get).Body.Bytes(), &d)
	if len(d.Violations) != 0 {
		t.Errorf("ignored violation should be hidden, got %+v", d.Violations)
	}
}

// TestSyncStatus checks the /api/sync panel data: null before any ingest, then
// last-batch counts + totals after.
func TestSyncStatus(t *testing.T) {
	mux, sdb := newMux(t)
	_, cookie, api := account(t, sdb, "noufal")

	// Before any ingest: lastSyncAt is null.
	req := httptest.NewRequest("GET", "/api/sync", nil)
	req.AddCookie(cookie)
	rec := do(mux, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"lastSyncAt":null`) {
		t.Fatalf("pre-ingest sync = %d %s", rec.Code, rec.Body.String())
	}

	ing := httptest.NewRequest("POST", "/api/ingest",
		strings.NewReader(`{"violations":[{"ts":1,"keyword":"k","url":"u"}],"heartbeat":[{"ts":2,"alive":true}]}`))
	ing.Header.Set("Authorization", "Bearer "+api)
	do(mux, ing)

	req = httptest.NewRequest("GET", "/api/sync", nil)
	req.AddCookie(cookie)
	var s struct {
		LastSyncAt *int64         `json:"lastSyncAt"`
		Last       map[string]int `json:"last"`
		Totals     map[string]int `json:"totals"`
	}
	json.Unmarshal(do(mux, req).Body.Bytes(), &s)
	if s.LastSyncAt == nil || *s.LastSyncAt == 0 {
		t.Error("lastSyncAt should be set after ingest")
	}
	if s.Last["violations"] != 1 || s.Last["heartbeat"] != 1 {
		t.Errorf("last batch counts = %+v", s.Last)
	}
	if s.Totals["violations"] != 1 {
		t.Errorf("totals = %+v", s.Totals)
	}
}

func TestRulesGetPut(t *testing.T) {
	mux, sdb := newMux(t)
	_, cookie, _ := account(t, sdb, "noufal")

	get := httptest.NewRequest("GET", "/api/rules", nil)
	get.AddCookie(cookie)
	if rec := do(mux, get); !strings.Contains(rec.Body.String(), `"rules":[]`) {
		t.Fatalf("empty GET = %s", rec.Body.String())
	}

	body := `{"rules":[{"program":"^Emacs$","title":"","tag":"Activity:work"},{"nope":true}],"colors":{"Activity:work":"#3aa0ff","x":"bad"}}`
	put := httptest.NewRequest("PUT", "/api/rules", strings.NewReader(body))
	put.AddCookie(cookie)
	if rec := do(mux, put); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}

	get2 := httptest.NewRequest("GET", "/api/rules", nil)
	get2.AddCookie(cookie)
	var cfg rulesConfig
	json.Unmarshal(do(mux, get2).Body.Bytes(), &cfg)
	if len(cfg.Rules) != 1 || cfg.Rules[0].Tag != "Activity:work" || cfg.Colors["Activity:work"] != "#3aa0ff" {
		t.Fatalf("persisted rules = %+v", cfg)
	}
	if len(cfg.Colors) != 1 {
		t.Errorf("bad colour should have been dropped: %+v", cfg.Colors)
	}
}

// TestSanitizeRulesAndColors covers the pure sanitizers.
func TestSanitizeRulesAndColors(t *testing.T) {
	rules := sanitizeRules([]Rule{
		{Program: " ^Emacs$ ", Title: "glocker", Tag: " Project:glocker "},
		{Tag: ""},
	})
	if len(rules) != 1 || rules[0].Program != "^Emacs$" || rules[0].Tag != "Project:glocker" {
		t.Fatalf("sanitizeRules = %+v", rules)
	}
	colors := sanitizeColors(map[string]string{"Activity:work": "#F0A020", "Bad:short": "#fff"})
	if len(colors) != 1 || colors["Activity:work"] != "#f0a020" {
		t.Fatalf("sanitizeColors = %+v", colors)
	}
}
