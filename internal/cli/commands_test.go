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

	if !strings.Contains(response, "RUNTIME STATUS") {
		t.Error("Response should contain 'RUNTIME STATUS'")
	}

	if !strings.Contains(response, "Currently Blocked Domains") {
		t.Error("Response should contain 'Currently Blocked Domains'")
	}
}

func TestGetStatusResponse_WithExtensionKeywords(t *testing.T) {
	cfg := &config.Config{
		Domains: []config.Domain{
			{Name: "example.com", AlwaysBlock: true},
		},
		ExtensionKeywords: config.ExtensionKeywordsConfig{
			URLKeywords:     []string{"gambling", "casino", "poker"},
			ContentKeywords: []string{"bet", "jackpot"},
			Whitelist:       []string{"example.com", "safe.com"},
		},
	}

	response := GetInfoResponse(cfg)

	// Should contain extension keywords section
	if !strings.Contains(response, "Extension Keywords:") {
		t.Error("Response should contain 'Extension Keywords:'")
	}

	// Should show URL keywords
	if !strings.Contains(response, "URL Keywords (3):") {
		t.Error("Response should show URL Keywords count")
	}
	if !strings.Contains(response, "gambling") || !strings.Contains(response, "casino") || !strings.Contains(response, "poker") {
		t.Error("Response should contain all URL keywords")
	}

	// Should show content keywords
	if !strings.Contains(response, "Content Keywords (2):") {
		t.Error("Response should show Content Keywords count")
	}
	if !strings.Contains(response, "bet") || !strings.Contains(response, "jackpot") {
		t.Error("Response should contain all content keywords")
	}

	// Should show whitelist count
	if !strings.Contains(response, "Whitelisted: 2 domains") {
		t.Error("Response should show whitelist count")
	}
}

func TestGetStatusResponse_WithManyKeywords(t *testing.T) {
	// Test with many keywords to verify all are shown
	manyKeywords := []string{"k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9", "k10", "k11", "k12"}

	cfg := &config.Config{
		Domains: []config.Domain{
			{Name: "example.com", AlwaysBlock: true},
		},
		ExtensionKeywords: config.ExtensionKeywordsConfig{
			URLKeywords: manyKeywords,
		},
	}

	response := GetInfoResponse(cfg)

	// Should show all keywords (no truncation)
	for _, keyword := range manyKeywords {
		if !strings.Contains(response, keyword) {
			t.Errorf("Response should contain keyword '%s'", keyword)
		}
	}

	// Should show correct count
	if !strings.Contains(response, "URL Keywords (12):") {
		t.Error("Response should show correct URL Keywords count")
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
