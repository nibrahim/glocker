package utils

import (
	"testing"
)

func TestIsIPAddress(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		isIP   bool
	}{
		{"valid IPv4", "192.168.1.1", true},
		{"valid IPv4 loopback", "127.0.0.1", true},
		{"valid IPv6", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", true},
		{"valid IPv6 short", "2001:db8::1", true},
		{"valid IPv6 loopback", "::1", true},
		{"invalid - domain name", "example.com", false},
		{"invalid - partial IP", "192.168.1", false},
		{"invalid - text", "not an ip", false},
		{"invalid - empty", "", false},
		{"invalid - too many octets", "192.168.1.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsIPAddress(tt.input)
			if result != tt.isIP {
				t.Errorf("IsIPAddress(%q) = %v, want %v", tt.input, result, tt.isIP)
			}
		})
	}
}

func TestResolveIPs(t *testing.T) {
	// Note: These tests depend on DNS being available and may fail in restricted environments
	// We test with localhost which should always resolve

	t.Run("resolve localhost IPv4", func(t *testing.T) {
		ips := ResolveIPs("localhost", "A")
		// We don't check specific IPs since DNS results can vary
		// Just verify the function doesn't panic and returns a list
		if ips == nil {
			t.Error("ResolveIPs should return non-nil slice")
		}
	})

	t.Run("resolve non-existent domain", func(t *testing.T) {
		ips := ResolveIPs("this-domain-definitely-does-not-exist-12345.com", "A")
		// Should return empty list, not nil
		if ips == nil {
			t.Error("ResolveIPs should return non-nil slice even for non-existent domains")
		}
	})

	t.Run("resolve with invalid record type", func(t *testing.T) {
		ips := ResolveIPs("localhost", "INVALID")
		// Should return empty list
		if ips == nil {
			t.Error("ResolveIPs should return non-nil slice for invalid record type")
		}
	})
}

func TestIsServiceRunning(t *testing.T) {
	// Test with a service that should not exist
	t.Run("non-existent service", func(t *testing.T) {
		result := IsServiceRunning("this-service-definitely-does-not-exist-12345")
		if result {
			t.Error("IsServiceRunning should return false for non-existent service")
		}
	})

	// Note: We can't reliably test with real services as they may or may not be running
	// In a real test environment, you'd use mocks or test fixtures
}
