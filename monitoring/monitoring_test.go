package monitoring

import (
	"os"
	"strings"
	"testing"
	"time"

	"glocker/config"
	"glocker/internal/state"
)

func TestCaptureChecksum(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-checksum-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := []byte("test content\n")
	if _, err := tmpFile.Write(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	cfg := &config.Config{
		HostsPath: "/etc/hosts", // Not the test file
	}

	// Capture checksum
	checksum := CaptureChecksum(cfg, tmpFile.Name())

	if !checksum.Exists {
		t.Error("Expected file to exist")
	}
	if checksum.Checksum == "" {
		t.Error("Expected non-empty checksum")
	}
	if checksum.Path != tmpFile.Name() {
		t.Errorf("Expected path %s, got %s", tmpFile.Name(), checksum.Path)
	}
}

func TestCaptureChecksum_NonExistent(t *testing.T) {
	cfg := &config.Config{}
	checksum := CaptureChecksum(cfg, "/nonexistent/file.txt")

	if checksum.Exists {
		t.Error("Expected file to not exist")
	}
	if checksum.Checksum != "" {
		t.Error("Expected empty checksum for non-existent file")
	}
}

func TestExtractGlockerSection(t *testing.T) {
	content := `127.0.0.1 localhost
127.0.1.1 myhost

### GLOCKER START ###
127.0.0.1 blocked.com
127.0.0.1 www.blocked.com
`

	section := ExtractGlockerSection(content)

	if !strings.Contains(section, "GLOCKER START") {
		t.Error("Expected glocker section to contain marker")
	}
	if !strings.Contains(section, "blocked.com") {
		t.Error("Expected glocker section to contain blocked domains")
	}
	if strings.Contains(section, "localhost") {
		t.Error("Expected glocker section to NOT contain non-glocker content")
	}
}

func TestRecordViolation(t *testing.T) {
	// Clear violations
	state.ClearViolations()

	cfg := &config.Config{
		ViolationTracking: config.ViolationTrackingConfig{
			Enabled:             true,
			MaxViolations:       5,
			TimeWindowMinutes:   60,
			Command:             "",
		},
	}

	// Record a violation
	RecordViolation(cfg, "web_access", "example.com", "https://example.com")

	// Check that it was recorded
	violations := state.GetViolations()
	if len(violations) != 1 {
		t.Fatalf("Expected 1 violation, got %d", len(violations))
	}

	if violations[0].Host != "example.com" {
		t.Errorf("Expected host example.com, got %s", violations[0].Host)
	}
	if violations[0].Type != "web_access" {
		t.Errorf("Expected type web_access, got %s", violations[0].Type)
	}

	// Clean up
	state.ClearViolations()
}

func TestCountRecentViolations(t *testing.T) {
	// Clear violations
	state.ClearViolations()

	cfg := &config.Config{
		ViolationTracking: config.ViolationTrackingConfig{
			Enabled:             true,
			TimeWindowMinutes:   60,
		},
	}

	now := time.Now()

	// Add some violations - some recent, some old
	state.AddViolation(state.Violation{
		Timestamp: now.Add(-10 * time.Minute), // Recent
		Host:      "recent1.com",
		Type:      "web_access",
	})
	state.AddViolation(state.Violation{
		Timestamp: now.Add(-30 * time.Minute), // Recent
		Host:      "recent2.com",
		Type:      "web_access",
	})
	state.AddViolation(state.Violation{
		Timestamp: now.Add(-90 * time.Minute), // Old (outside 60 min window)
		Host:      "old.com",
		Type:      "web_access",
	})

	count := countRecentViolations(cfg, now)

	if count != 2 {
		t.Errorf("Expected 2 recent violations, got %d", count)
	}

	// Clean up
	state.ClearViolations()
}

func TestRecordViolation_Disabled(t *testing.T) {
	// Clear violations
	state.ClearViolations()

	cfg := &config.Config{
		ViolationTracking: config.ViolationTrackingConfig{
			Enabled: false,
		},
	}

	// Record a violation
	RecordViolation(cfg, "web_access", "example.com", "https://example.com")

	// Check that it was NOT recorded (disabled)
	violations := state.GetViolations()
	if len(violations) != 0 {
		t.Errorf("Expected 0 violations when disabled, got %d", len(violations))
	}
}

func TestExtractProcessName(t *testing.T) {
	// Test with a typical ps aux line
	psLine := "user     12345  0.0  0.1  12345  6789 ?        S    10:00   0:00 /usr/bin/firefox"

	name := extractProcessName(psLine)

	if name != "/usr/bin/firefox" {
		t.Errorf("Expected /usr/bin/firefox, got %s", name)
	}
}

func TestExtractProcessName_Short(t *testing.T) {
	// Test with insufficient fields
	psLine := "user 12345"

	name := extractProcessName(psLine)

	if name != "unknown" {
		t.Errorf("Expected 'unknown' for short line, got %s", name)
	}
}
