package cli

import (
	"strings"
	"testing"
	"time"

	"glocker/config"
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
