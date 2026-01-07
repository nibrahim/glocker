package enforcement

import (
	"log/slog"
	"net"
	"os/exec"

	"glocker/internal/utils"
)

// UpdateFirewall updates iptables and ip6tables rules to block specified domains and IPs.
// It resolves domain names to IP addresses and creates firewall rules for both IPv4 and IPv6.
func UpdateFirewall(domains []string, dryRun bool) error {
	slog.Debug("Starting firewall update", "domains_count", len(domains), "dry_run", dryRun)

	if dryRun {
		slog.Debug("Dry run mode - would update firewall rules")
		return nil
	}

	// Clear old rules with our marker
	slog.Debug("Clearing old IPv4 firewall rules")
	clearCmd := `iptables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | while read rule; do iptables $rule 2>/dev/null; done`
	exec.Command("bash", "-c", clearCmd).Run()

	// Also clear ip6tables rules
	slog.Debug("Clearing old IPv6 firewall rules")
	clearCmd6 := `ip6tables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | while read rule; do ip6tables $rule 2>/dev/null; done`
	exec.Command("bash", "-c", clearCmd6).Run()

	totalIPs := 0
	for _, domain := range domains {
		slog.Debug("Processing entry for firewall blocking", "entry", domain)

		if utils.IsIPAddress(domain) {
			// It's an IP address, block it directly
			slog.Debug("Entry is an IP address, blocking directly", "ip", domain)

			// Determine if it's IPv4 or IPv6
			ip := net.ParseIP(domain)
			if ip == nil {
				slog.Debug("Failed to parse IP address", "ip", domain)
				continue
			}

			if ip.To4() != nil {
				// IPv4 address
				cmd := exec.Command("iptables", "-I", "OUTPUT", "-d", domain,
					"-j", "REJECT", "--reject-with", "icmp-host-unreachable",
					"-m", "comment", "--comment", "GLOCKER-BLOCK")

				if err := cmd.Run(); err == nil {
					totalIPs++
					slog.Debug("Added IPv4 firewall rule for IP", "ip", domain)
				} else {
					slog.Debug("Failed to add IPv4 firewall rule for IP", "ip", domain, "error", err)
				}
			} else {
				// IPv6 address
				cmd := exec.Command("ip6tables", "-I", "OUTPUT", "-d", domain,
					"-j", "REJECT", "--reject-with", "icmp6-adm-prohibited",
					"-m", "comment", "--comment", "GLOCKER-BLOCK")

				if err := cmd.Run(); err == nil {
					totalIPs++
					slog.Debug("Added IPv6 firewall rule for IP", "ip", domain)
				} else {
					slog.Debug("Failed to add IPv6 firewall rule for IP", "ip", domain, "error", err)
				}
			}
		} else {
			// It's a hostname, resolve and block
			slog.Debug("Entry is a hostname, resolving", "hostname", domain)

			// Resolve and block IPv4 addresses
			ips := utils.ResolveIPs(domain, "A")
			slog.Debug("Resolved IPv4 addresses", "domain", domain, "ips", ips)

			for _, ip := range ips {
				cmd := exec.Command("iptables", "-I", "OUTPUT", "-d", ip,
					"-j", "REJECT", "--reject-with", "icmp-host-unreachable",
					"-m", "comment", "--comment", "GLOCKER-BLOCK")

				if err := cmd.Run(); err == nil {
					totalIPs++
					slog.Debug("Added IPv4 firewall rule", "domain", domain, "ip", ip)
				} else {
					slog.Debug("Failed to add IPv4 firewall rule", "domain", domain, "ip", ip, "error", err)
				}
			}

			// Resolve and block IPv6 addresses
			ips6 := utils.ResolveIPs(domain, "AAAA")
			slog.Debug("Resolved IPv6 addresses", "domain", domain, "ips", ips6)

			for _, ip := range ips6 {
				cmd := exec.Command("ip6tables", "-I", "OUTPUT", "-d", ip,
					"-j", "REJECT", "--reject-with", "icmp6-adm-prohibited",
					"-m", "comment", "--comment", "GLOCKER-BLOCK")

				if err := cmd.Run(); err == nil {
					totalIPs++
					slog.Debug("Added IPv6 firewall rule", "domain", domain, "ip", ip)
				} else {
					slog.Debug("Failed to add IPv6 firewall rule", "domain", domain, "ip", ip, "error", err)
				}
			}
		}
	}

	slog.Debug("Firewall update completed", "total_ips_blocked", totalIPs)
	return nil
}
