package utils

import "testing"

func TestIsValidTime(t *testing.T) {
	tests := []struct {
		name  string
		time  string
		valid bool
	}{
		{"valid time 09:00", "09:00", true},
		{"valid time 23:59", "23:59", true},
		{"valid time 00:00", "00:00", true},
		{"valid time 9:00", "9:00", true},
		{"invalid hour", "25:00", false},
		{"invalid minute", "12:60", false},
		{"not a time", "abc", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTime(tt.time)
			if result != tt.valid {
				t.Errorf("IsValidTime(%q) = %v, want %v", tt.time, result, tt.valid)
			}
		})
	}
}

func TestIsInTimeWindow(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		start    string
		end      string
		expected bool
	}{
		// Normal windows (start < end)
		{"within normal window", "12:00", "09:00", "17:00", true},
		{"at start of normal window", "09:00", "09:00", "17:00", true},
		{"at end of normal window", "17:00", "09:00", "17:00", true},
		{"before normal window", "08:00", "09:00", "17:00", false},
		{"after normal window", "18:00", "09:00", "17:00", false},

		// Wraparound windows (start > end, crosses midnight)
		{"within wraparound window - evening", "23:00", "22:00", "02:00", true},
		{"within wraparound window - morning", "01:00", "22:00", "02:00", true},
		{"at start of wraparound window", "22:00", "22:00", "02:00", true},
		{"at end of wraparound window", "02:00", "22:00", "02:00", true},
		{"outside wraparound window", "12:00", "22:00", "02:00", false},

		// Edge cases
		{"midnight to midnight", "00:00", "00:00", "00:00", true},
		{"almost full day", "12:00", "00:00", "23:59", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInTimeWindow(tt.current, tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("IsInTimeWindow(%q, %q, %q) = %v, want %v",
					tt.current, tt.start, tt.end, result, tt.expected)
			}
		})
	}
}
