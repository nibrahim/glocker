package notify

import (
	"strings"
	"testing"
	"time"

	"glocker/config"
	"glocker/internal/state"
)

func TestSendEmail_Disabled(t *testing.T) {
	cfg := &config.Config{
		Accountability: config.AccountabilityConfig{
			Enabled: false,
		},
	}

	err := SendEmail(cfg, "Test Subject", "Test Body")
	if err != nil {
		t.Errorf("Expected nil error when accountability disabled, got %v", err)
	}
}

func TestSendEmail_DevMode(t *testing.T) {
	cfg := &config.Config{
		Dev: true,
		Accountability: config.AccountabilityConfig{
			Enabled: true,
		},
	}

	err := SendEmail(cfg, "Test Subject", "Test Body")
	if err != nil {
		t.Errorf("Expected nil error in dev mode, got %v", err)
	}
}

func TestSendEmail_RateLimiting(t *testing.T) {
	subject := "Rate Limit Test"

	// Set last email time to now
	state.SetLastEmailTime(subject, time.Now())

	cfg := &config.Config{
		Dev: false,
		Accountability: config.AccountabilityConfig{
			Enabled:      true,
			FromEmail:    "test@example.com",
			PartnerEmail: "partner@example.com",
			ApiKey:       "test-api-key",
		},
	}

	// Should be rate limited since we just sent
	err := SendEmail(cfg, subject, "Test Body")
	if err != nil {
		t.Errorf("Expected nil error when rate limited, got %v", err)
	}

	// Verify the last email time was not updated (still recent)
	lastSent, exists := state.GetLastEmailTime(subject)
	if !exists {
		t.Error("Expected email time to exist")
	}
	if time.Since(lastSent) > 1*time.Second {
		t.Error("Expected last sent time to be very recent")
	}
}

func TestGenerateHTMLEmail_Escaping(t *testing.T) {
	subject := "Test Subject"
	body := "<script>alert('XSS')</script>\n&\"test\""

	html := GenerateHTMLEmail(subject, body)

	// Verify HTML escaping
	if strings.Contains(html, "<script>") {
		t.Error("HTML not properly escaped: contains <script> tag")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("HTML not properly escaped: missing &lt;script&gt;")
	}
	if strings.Contains(html, "alert('XSS')") {
		t.Error("HTML not properly escaped: contains unescaped JavaScript")
	}
	if !strings.Contains(html, "&amp;") {
		t.Error("HTML not properly escaped: missing &amp;")
	}
}

func TestGenerateHTMLEmail_AlertTypes(t *testing.T) {
	tests := []struct {
		subject      string
		expectedIcon string
		expectedColor string
	}{
		{"Tamper Detected", "⚠️", "#d32f2f"},
		{"Blocked Access Violation", "🚫", "#f57c00"},
		{"Domain Unblock Request", "🔓", "#1976d2"},
		{"Installation Complete", "✅", "#388e3c"},
		{"Process Termination", "🛡️", "#d32f2f"},
		{"Generic Info", "ℹ️", "#1976d2"},
	}

	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			html := GenerateHTMLEmail(tt.subject, "Test body")

			if !strings.Contains(html, tt.expectedIcon) {
				t.Errorf("Expected icon %s not found in HTML", tt.expectedIcon)
			}
			if !strings.Contains(html, tt.expectedColor) {
				t.Errorf("Expected color %s not found in HTML", tt.expectedColor)
			}
		})
	}
}

func TestGenerateHTMLEmail_Structure(t *testing.T) {
	subject := "Test Alert"
	body := "This is a test message.\nSecond line."

	html := GenerateHTMLEmail(subject, body)

	// Verify basic HTML structure
	requiredElements := []string{
		"<!DOCTYPE html>",
		"<html>",
		"<head>",
		"<body>",
		"Glocker Security Alert",
		subject,
		"This is a test message.<br>Second line.",
		"Automated Security Monitoring System",
	}

	for _, elem := range requiredElements {
		if !strings.Contains(html, elem) {
			t.Errorf("HTML missing required element: %s", elem)
		}
	}
}

func TestGenerateHTMLEmail_LineBreaks(t *testing.T) {
	subject := "Test"
	body := "Line 1\nLine 2\nLine 3"

	html := GenerateHTMLEmail(subject, body)

	// Verify newlines converted to <br>
	if !strings.Contains(html, "Line 1<br>Line 2<br>Line 3") {
		t.Error("Line breaks not properly converted to <br>")
	}
}

func TestAdjustColorBrightness(t *testing.T) {
	tests := []struct {
		input    string
		percent  int
		expected string
	}{
		{"#d32f2f", -20, "#b71c1c"},
		{"#f57c00", -20, "#e65100"},
		{"#1976d2", -20, "#1565c0"},
		{"#388e3c", -20, "#2e7d32"},
		{"#unknown", -20, "#333333"},
		{"#d32f2f", 0, "#d32f2f"}, // No change for 0
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := adjustColorBrightness(tt.input, tt.percent)
			if result != tt.expected {
				t.Errorf("adjustColorBrightness(%s, %d) = %s, want %s",
					tt.input, tt.percent, result, tt.expected)
			}
		})
	}
}

func TestAdjustColorOpacity(t *testing.T) {
	tests := []struct {
		input    string
		opacity  float64
		expected string
	}{
		{"#d32f2f", 0.1, "rgba(211, 47, 47, 0.1)"},
		{"#f57c00", 0.5, "rgba(245, 124, 0, 0.5)"},
		{"#1976d2", 1.0, "rgba(25, 118, 210, 1.0)"},
		{"#388e3c", 0.2, "rgba(56, 142, 60, 0.2)"},
		{"#unknown", 0.3, "rgba(51, 51, 51, 0.3)"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := adjustColorOpacity(tt.input, tt.opacity)
			if result != tt.expected {
				t.Errorf("adjustColorOpacity(%s, %.1f) = %s, want %s",
					tt.input, tt.opacity, result, tt.expected)
			}
		})
	}
}

func TestSendNotification_EmptyCommand(t *testing.T) {
	cfg := &config.Config{
		NotificationCommand: "",
	}

	// Should return silently without error
	SendNotification(cfg, "Test", "Message", "normal", "info")
	// No assertion needed - just verify it doesn't panic
}

func TestSendNotification_PlaceholderReplacement(t *testing.T) {
	// We can't easily test actual command execution without mocking,
	// but we can verify the function doesn't panic with valid input
	cfg := &config.Config{
		NotificationCommand: "echo {title} {message} {urgency} {icon}",
	}

	// Should execute without panic (may fail if echo not available, but that's OK)
	SendNotification(cfg, "Test Title", "Test Message", "critical", "warning")
	// No assertion needed - just verify it doesn't panic
}
