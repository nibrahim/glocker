package cli

import (
	"strings"
	"testing"
	"time"

	"glocker/internal/config"
	"glocker/internal/state"
)

func TestGetStatusResponse(t *testing.T) {
	cfg := &config.Config{
		Domains: []config.Domain{
			{Name: "example.com", AlwaysBlock: true},
		},
	}

	response := GetStatusResponse(cfg)

	if !strings.Contains(response, "LIVE STATUS") {
		t.Error("Response should contain 'LIVE STATUS'")
	}

	if !strings.Contains(response, "Currently Blocking") {
		t.Error("Response should contain 'Currently Blocking'")
	}
}

func TestProcessPanicRequest(t *testing.T) {
	cfg := &config.Config{}

	// Clear panic state
	state.SetPanicUntil(time.Time{})

	// Process panic request
	ProcessPanicRequest(cfg, 5)

	// Check that panic mode was set
	panicUntil := state.GetPanicUntil()
	if panicUntil.IsZero() {
		t.Error("Panic mode should be set")
	}

	// Verify it's approximately 5 minutes from now
	now := time.Now()
	expectedUntil := now.Add(5 * time.Minute)
	diff := panicUntil.Sub(expectedUntil).Abs()

	if diff > 5*time.Second {
		t.Errorf("Panic until time differs by %v, expected within 5 seconds", diff)
	}
}

func TestProcessUnblockRequest_RejectsAbsoluteDomains(t *testing.T) {
	cfg := &config.Config{
		Domains: []config.Domain{
			{Name: "absolute.com", AlwaysBlock: true, Absolute: true},
			{Name: "regular.com", AlwaysBlock: true, Absolute: false},
		},
		Unblocking: config.UnblockingConfig{
			TempUnblockTime: 30,
		},
	}

	// Clear temp unblocks
	state.SetTempUnblocks([]state.TempUnblock{})

	// Try to unblock both domains
	ProcessUnblockRequest(cfg, "absolute.com,regular.com", "testing")

	// Check temp unblocks
	unblocks := state.GetTempUnblocks()

	// Should only have regular.com, not absolute.com
	if len(unblocks) != 1 {
		t.Errorf("Expected 1 temp unblock, got %d", len(unblocks))
	}

	if len(unblocks) > 0 && unblocks[0].Domain != "regular.com" {
		t.Errorf("Expected regular.com to be unblocked, got %s", unblocks[0].Domain)
	}

	// Verify absolute.com is NOT in temp unblocks
	for _, unblock := range unblocks {
		if unblock.Domain == "absolute.com" {
			t.Error("absolute.com should not be in temp unblocks")
		}
	}
}
