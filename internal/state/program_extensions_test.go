package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetProgramExtensions clears in-memory state. Tests redirect
// ProgramExtensionsPath to a tempdir before calling this.
func resetProgramExtensions(t *testing.T) {
	t.Helper()
	programExtensionsMutex.Lock()
	programExtensions = nil
	programExtensionsMutex.Unlock()
}

// withTempExtensionsPath redirects persistence to a per-test temp file and
// restores the original on cleanup.
func withTempExtensionsPath(t *testing.T) string {
	t.Helper()
	orig := ProgramExtensionsPath
	dir := t.TempDir()
	ProgramExtensionsPath = filepath.Join(dir, "extensions.json")
	t.Cleanup(func() {
		ProgramExtensionsPath = orig
		resetProgramExtensions(t)
	})
	resetProgramExtensions(t)
	return ProgramExtensionsPath
}

func TestAddAndGetActiveExtension(t *testing.T) {
	withTempExtensionsPath(t)

	now := time.Now()
	ext := ProgramExtension{
		Program:   "firefox-esr",
		GrantedAt: now,
		ExpiresAt: now.Add(ExtensionDuration),
		Reason:    "client call",
	}
	if err := AddProgramExtension(ext); err != nil {
		t.Fatalf("AddProgramExtension: %v", err)
	}

	got, ok := GetActiveExtension("firefox-esr")
	if !ok {
		t.Fatal("expected active extension for firefox-esr")
	}
	if got.Reason != "client call" {
		t.Errorf("got reason %q, want %q", got.Reason, "client call")
	}

	if _, ok := GetActiveExtension("chrome"); ok {
		t.Error("did not expect active extension for chrome")
	}
}

func TestExpiredExtensionNotActive(t *testing.T) {
	withTempExtensionsPath(t)

	past := time.Now().Add(-2 * time.Hour)
	if err := AddProgramExtension(ProgramExtension{
		Program:   "firefox-esr",
		GrantedAt: past,
		ExpiresAt: past.Add(ExtensionDuration), // expired 1h ago
		Reason:    "old",
	}); err != nil {
		t.Fatalf("AddProgramExtension: %v", err)
	}

	if _, ok := GetActiveExtension("firefox-esr"); ok {
		t.Error("expired extension should not be active")
	}
}

func TestLastExtensionGrantTracksMostRecent(t *testing.T) {
	withTempExtensionsPath(t)

	now := time.Now()
	older := now.Add(-10 * time.Hour)

	for _, g := range []time.Time{older, now.Add(-5 * time.Hour), now.Add(-2 * time.Hour)} {
		if err := AddProgramExtension(ProgramExtension{
			Program:   "firefox-esr",
			GrantedAt: g,
			ExpiresAt: g.Add(ExtensionDuration),
			Reason:    "test",
		}); err != nil {
			t.Fatalf("AddProgramExtension: %v", err)
		}
	}

	last, ok := GetLastExtensionGrant("firefox-esr")
	if !ok {
		t.Fatal("expected a last grant")
	}
	wantAround := now.Add(-2 * time.Hour)
	if last.Before(wantAround.Add(-time.Second)) || last.After(wantAround.Add(time.Second)) {
		t.Errorf("last = %v, want approximately %v", last, wantAround)
	}

	if _, ok := GetLastExtensionGrant("chrome"); ok {
		t.Error("did not expect last grant for chrome")
	}
}

func TestCooldownPruningDropsOldGrants(t *testing.T) {
	withTempExtensionsPath(t)

	// Grants older than the cooldown window should be pruned after the
	// next Add. This keeps the persisted file small and stops ancient
	// grants from confusing cooldown logic if the system clock skews.
	ancient := time.Now().Add(-48 * time.Hour)
	if err := AddProgramExtension(ProgramExtension{
		Program:   "firefox-esr",
		GrantedAt: ancient,
		ExpiresAt: ancient.Add(ExtensionDuration),
	}); err != nil {
		t.Fatalf("AddProgramExtension: %v", err)
	}

	// Trigger pruning by adding a fresh grant.
	if err := AddProgramExtension(ProgramExtension{
		Program:   "firefox-esr",
		GrantedAt: time.Now(),
		ExpiresAt: time.Now().Add(ExtensionDuration),
	}); err != nil {
		t.Fatalf("AddProgramExtension: %v", err)
	}

	exts := GetProgramExtensions()
	if len(exts) != 1 {
		t.Errorf("expected 1 retained grant after pruning, got %d", len(exts))
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := withTempExtensionsPath(t)

	now := time.Now()
	if err := AddProgramExtension(ProgramExtension{
		Program:   "firefox-esr",
		GrantedAt: now,
		ExpiresAt: now.Add(ExtensionDuration),
		Reason:    "client call",
	}); err != nil {
		t.Fatalf("AddProgramExtension: %v", err)
	}

	// File must exist after Add.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("extensions file missing after Add: %v", err)
	}

	// Drop in-memory state, then Load and re-check.
	resetProgramExtensions(t)
	if _, ok := GetActiveExtension("firefox-esr"); ok {
		t.Fatal("reset did not clear in-memory state")
	}

	if err := LoadProgramExtensions(); err != nil {
		t.Fatalf("LoadProgramExtensions: %v", err)
	}
	if _, ok := GetActiveExtension("firefox-esr"); !ok {
		t.Error("expected active extension to be restored from disk")
	}
}

func TestLoadMissingFileIsNotError(t *testing.T) {
	withTempExtensionsPath(t)
	// File deliberately not written.
	if err := LoadProgramExtensions(); err != nil {
		t.Errorf("LoadProgramExtensions on missing file should not error, got %v", err)
	}
	if exts := GetProgramExtensions(); len(exts) != 0 {
		t.Errorf("expected no extensions after loading missing file, got %d", len(exts))
	}
}
