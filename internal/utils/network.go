package utils

import (
	"net"
	"os/exec"
	"strings"
)

// IsIPAddress checks if a string is a valid IPv4 or IPv6 address.
func IsIPAddress(s string) bool {
	// Try to parse as IPv4 or IPv6
	return net.ParseIP(s) != nil
}

// ResolveIPs resolves a domain name to IP addresses using DNS.
// recordType should be "A" for IPv4 or "AAAA" for IPv6.
// Returns a list of IP addresses, or an empty list if resolution fails.
func ResolveIPs(domain string, recordType string) []string {
	ips := make([]string, 0)

	// Try to resolve the domain using dig
	cmd := exec.Command("dig", "+short", domain, recordType)
	output, err := cmd.Output()
	if err != nil {
		return ips
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		ip := strings.TrimSpace(line)

		// Skip empty lines
		if ip == "" {
			continue
		}

		// Validate IPv4
		if recordType == "A" {
			if strings.Count(ip, ".") == 3 && !strings.Contains(ip, " ") && len(ip) >= 7 {
				ips = append(ips, ip)
			}
		}

		// Validate IPv6 (basic check for colon)
		if recordType == "AAAA" {
			if strings.Contains(ip, ":") && !strings.Contains(ip, " ") {
				ips = append(ips, ip)
			}
		}
	}

	return ips
}

// IsServiceRunning checks if a systemd service is running.
func IsServiceRunning(serviceName string) bool {
	cmd := exec.Command("systemctl", "is-active", serviceName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "active"
}
