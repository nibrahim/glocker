package utils

import "time"

// IsValidTime checks if a time string is in valid HH:MM format.
// Returns true if the string can be parsed as a time, false otherwise.
func IsValidTime(timeStr string) bool {
	_, err := time.Parse("15:04", timeStr)
	return err == nil
}

// IsInTimeWindow checks if the current time falls within a time window.
// Handles wraparound cases where the end time is before the start time (e.g., 22:00-02:00).
// All times should be in HH:MM format.
func IsInTimeWindow(current, start, end string) bool {
	// Simple string comparison works for HH:MM format
	if start <= end {
		// Normal case: 09:00 - 17:00
		return current >= start && current <= end
	}
	// Wraparound case: 22:00 - 02:00
	return current >= start || current <= end
}
