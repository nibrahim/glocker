package store

import (
	"path/filepath"
	"testing"
)

// openMem opens a fresh temp-file sqlite store for a test.
func openMem(t *testing.T) *DB {
	t.Helper()
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

func TestIngestIdempotent(t *testing.T) {
	db := openMem(t)
	const uid = 1
	v := []Violation{{TS: 1000, Keyword: "porn", URL: "https://x/?q=porn", Type: "url-keyword", Domain: "x"}}
	if err := db.IngestViolations(uid, v); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// Re-ingesting the same (user,ts,keyword,url) must not duplicate.
	if err := db.IngestViolations(uid, []Violation{{TS: 1000, Keyword: "porn", URL: "https://x/?q=porn"}}); err != nil {
		t.Fatalf("re-ingest: %v", err)
	}
	got, _ := db.Violations(uid)
	if len(got) != 1 {
		t.Fatalf("want 1 violation after dup ingest, got %d", len(got))
	}

	// Usage + heartbeat unique on (user, ts).
	if err := db.IngestUsage(uid, []UsageSample{{TS: 5, ActiveClass: "kitty"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.IngestUsage(uid, []UsageSample{{TS: 5, ActiveClass: "firefox"}}); err != nil {
		t.Fatal(err)
	}
	us, _ := db.UsageSamples(uid)
	if len(us) != 1 {
		t.Errorf("want 1 usage sample, got %d", len(us))
	}
}

// TestTenantIsolation is the crux of the multi-tenant model: one account never
// sees another's rows, even with identical natural keys.
func TestTenantIsolation(t *testing.T) {
	db := openMem(t)
	const alice, bob = 1, 2

	same := Violation{TS: 1000, Keyword: "porn", URL: "https://x/?q=porn", Domain: "x"}
	if err := db.IngestViolations(alice, []Violation{same}); err != nil {
		t.Fatal(err)
	}
	// Bob ingests the identical record — must NOT collide with Alice's (the
	// unique key is per-user), and must land in Bob's data only.
	if err := db.IngestViolations(bob, []Violation{same}); err != nil {
		t.Fatalf("bob ingest identical record: %v", err)
	}

	av, _ := db.Violations(alice)
	bv, _ := db.Violations(bob)
	if len(av) != 1 || len(bv) != 1 {
		t.Fatalf("each account should see exactly its own: alice=%d bob=%d", len(av), len(bv))
	}

	// Bob's ignore overlay must not affect Alice.
	if err := db.SetIgnored(bob, []IgnoredViolation{{TS: 1000, Keyword: "porn", URL: "https://x/?q=porn"}}); err != nil {
		t.Fatal(err)
	}
	ai, _ := db.IgnoredViolations(alice)
	bi, _ := db.IgnoredViolations(bob)
	if len(ai) != 0 || len(bi) != 1 {
		t.Errorf("ignore overlay leaked across tenants: alice=%d bob=%d", len(ai), len(bi))
	}

	// Rules are per-account too.
	if err := db.SetRulesConfig(alice, []Rule{{Program: "Emacs", Tag: "work"}}, map[string]string{"work": "#4c9f70"}); err != nil {
		t.Fatal(err)
	}
	ar, _ := db.Rules(alice)
	br, _ := db.Rules(bob)
	if len(ar) != 1 || len(br) != 0 {
		t.Errorf("rules leaked across tenants: alice=%d bob=%d", len(ar), len(br))
	}
}

func TestUsersSessionsTokens(t *testing.T) {
	db := openMem(t)

	u, err := db.CreateUser("noufal", "correct horse battery staple")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Good + bad password.
	if _, err := db.Authenticate("noufal", "correct horse battery staple"); err != nil {
		t.Errorf("valid login rejected: %v", err)
	}
	if _, err := db.Authenticate("noufal", "wrong"); err != ErrInvalidCredentials {
		t.Errorf("bad password: want ErrInvalidCredentials, got %v", err)
	}
	if _, err := db.Authenticate("ghost", "x"); err != ErrInvalidCredentials {
		t.Errorf("unknown user: want ErrInvalidCredentials, got %v", err)
	}

	// Session round trip + logout.
	tok, err := db.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.UserBySession(tok); err != nil || got.ID != u.ID {
		t.Errorf("session lookup: %v (user %+v)", err, got)
	}
	if err := db.DeleteSession(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserBySession(tok); err != ErrInvalidCredentials {
		t.Errorf("deleted session should be invalid, got %v", err)
	}

	// API token round trip; wrong token rejected.
	api, err := db.CreateAPIToken(u.ID, "syncer")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.UserByAPIToken(api); err != nil || got.ID != u.ID {
		t.Errorf("api token lookup: %v (user %+v)", err, got)
	}
	if _, err := db.UserByAPIToken("nope"); err != ErrInvalidCredentials {
		t.Errorf("bad api token: want ErrInvalidCredentials, got %v", err)
	}
}
