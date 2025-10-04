package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed conf/conf.yaml
var configData []byte

const (
	INSTALL_PATH       = "/usr/local/bin/glocker"
	HOSTS_MARKER_START = "### DISTRACTION BLOCKER START ###"
	HOSTS_MARKER_END   = "### DISTRACTION BLOCKER END ###"
	SUDOERS_PATH       = "/etc/sudoers"
	SUDOERS_BACKUP     = "/etc/sudoers.distraction-blocker.backup"
	SUDOERS_MARKER     = "# DISTRACTION-BLOCKER-MANAGED"
)

type TimeWindow struct {
	Start string   `yaml:"start"` // HH:MM format
	End   string   `yaml:"end"`   // HH:MM format
	Days  []string `yaml:"days"`  // Mon, Tue, Wed, Thu, Fri, Sat, Sun
}

type Domain struct {
	Name        string       `yaml:"name"`
	AlwaysBlock bool         `yaml:"always_block"`
	TimeWindows []TimeWindow `yaml:"time_windows,omitempty"`
}

type SudoersConfig struct {
	Enabled            bool         `yaml:"enabled"`
	User               string       `yaml:"user"`
	AllowedSudoersLine string       `yaml:"allowed_sudoers_line"`
	BlockedSudoersLine string       `yaml:"blocked_sudoers_line"`
	TimeAllowed        []TimeWindow `yaml:"time_allowed"`
}

type Config struct {
	EnableHosts     bool          `yaml:"enable_hosts"`
	EnableFirewall  bool          `yaml:"enable_firewall"`
	Domains         []Domain      `yaml:"domains"`
	HostsPath       string        `yaml:"hosts_path"`
	SelfHeal        bool          `yaml:"self_heal"`
	EnforceInterval int           `yaml:"enforce_interval_seconds"`
	Sudoers         SudoersConfig `yaml:"sudoers"`
}

func main() {
	dryRun := flag.Bool("dry-run", false, "Show what would be done without making changes")
	enforce := flag.Bool("enforce", false, "Run enforcement loop (runs continuously)")
	once := flag.Bool("once", false, "Run enforcement once and exit")
	install := flag.Bool("install", false, "Install protection on the binary itself")
	flag.Parse()

	// Parse embedded config
	var config Config
	if err := yaml.Unmarshal(configData, &config); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// Set defaults
	if config.HostsPath == "" {
		config.HostsPath = "/etc/hosts"
	}
	if config.EnforceInterval == 0 {
		config.EnforceInterval = 60
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}

	log.Printf("Loaded config: %d domains", len(config.Domains))
	log.Printf("Components enabled - Hosts: %v, Firewall: %v, Sudoers: %v, Self-Heal: %v",
		config.EnableHosts, config.EnableFirewall, config.Sudoers.Enabled, config.SelfHeal)

	if *install {
		installProtections(&config)
		return
	}

	if *dryRun {
		log.Println("=== DRY RUN MODE ===")
		runOnce(&config, true)
		return
	}

	if *once {
		runOnce(&config, false)
		return
	}

	if *enforce {
		log.Println("Starting enforcement loop...")
		log.Printf("Enforcement interval: %d seconds", config.EnforceInterval)

		// Initial enforcement
		runOnce(&config, false)

		// Continuous loop
		ticker := time.NewTicker(time.Duration(config.EnforceInterval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			runOnce(&config, false)
		}
		return
	}

	// Default: show status
	showStatus(&config)
}

func runOnce(config *Config, dryRun bool) {
	now := time.Now()
	blockedDomains := getDomainsToBlock(config, now)

	log.Printf("[%s] Blocking %d domains", now.Format("15:04:05"), len(blockedDomains))

	// Self-healing: verify our own integrity
	if config.SelfHeal && !dryRun {
		selfHeal()
	}

	if config.EnableHosts {
		if err := updateHosts(config, blockedDomains, dryRun); err != nil {
			log.Printf("ERROR updating hosts: %v", err)
		}
	}

	if config.EnableFirewall {
		if err := updateFirewall(blockedDomains, dryRun); err != nil {
			log.Printf("ERROR updating firewall: %v", err)
		}
	}

	if config.Sudoers.Enabled {
		if err := updateSudoers(config, now, dryRun); err != nil {
			log.Printf("ERROR updating sudoers: %v", err)
		}
	}
}

func selfHeal() {
	log.Printf("Testing binary integrity")
	// Check if our binary still exists
	if _, err := os.Stat(INSTALL_PATH); os.IsNotExist(err) {
		log.Fatal("CRITICAL: Distraction blocker binary was deleted! Self-healing failed.")
	}

	// Re-apply immutable flag on our binary
	if err := exec.Command("chattr", "+i", INSTALL_PATH).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable on binary: %v", err)
	}

	// Verify we're still running as the expected process
	exe, err := os.Executable()
	if err == nil {
		exePath, _ := filepath.EvalSymlinks(exe)
		if exePath != INSTALL_PATH {
			log.Printf("Warning: running from unexpected location: %s (expected %s)", exePath, INSTALL_PATH)
		}
	}
}

func installProtections(config *Config) {
	log.Println("Installing protections on binary...")

	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	exePath, err := filepath.EvalSymlinks(exe)
	if err != nil {
		log.Fatalf("Failed to resolve executable path: %v", err)
	}

	if exePath != INSTALL_PATH {
		log.Printf("Warning: running from %s, not from install location %s", exePath, INSTALL_PATH)
		log.Println("Protections will be applied to the install location.")
	}

	// Set ownership to root:root
	if err := os.Chown(INSTALL_PATH, 0, 0); err != nil {
		log.Printf("Warning: couldn't set ownership to root: %v", err)
	} else {
		log.Printf("Set ownership to root:root on %s", INSTALL_PATH)
	}

	// Set setuid bit (4755 = rwsr-xr-x)
	if err := os.Chmod(INSTALL_PATH, 0o4755); err != nil {
		log.Printf("Warning: couldn't set setuid bit: %v", err)
	} else {
		log.Printf("Set setuid bit on %s", INSTALL_PATH)
	}

	// Set immutable on the installed binary
	if err := exec.Command("chattr", "+i", INSTALL_PATH).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable flag: %v", err)
	} else {
		log.Printf("Set immutable flag on %s", INSTALL_PATH)
	}

	// Protect the systemd service file if it exists
	servicePath := "/etc/systemd/system/distraction-blocker.service"
	if _, err := os.Stat(servicePath); err == nil {
		if err := exec.Command("chattr", "+i", servicePath).Run(); err != nil {
			log.Printf("Warning: couldn't protect service file: %v", err)
		} else {
			log.Printf("Protected service file: %s", servicePath)
		}
	}

	// Create sudoers backup if sudoers management is enabled
	if config.Sudoers.Enabled {
		if err := createSudoersBackup(); err != nil {
			log.Printf("Warning: couldn't create sudoers backup: %v", err)
		} else {
			log.Printf("Created sudoers backup at %s", SUDOERS_BACKUP)
		}
	}

	log.Println("Installation complete!")
}

func updateSudoers(config *Config, now time.Time, dryRun bool) error {
	if !config.Sudoers.Enabled {
		return nil
	}

	// Determine if sudo should be allowed at this time
	sudoAllowed := isSudoAllowed(config, now)

	targetLine := config.Sudoers.BlockedSudoersLine
	if sudoAllowed {
		targetLine = config.Sudoers.AllowedSudoersLine
	}

	log.Printf("Sudoers management: sudo %s for user %s",
		map[bool]string{true: "ALLOWED", false: "BLOCKED"}[sudoAllowed],
		config.Sudoers.User)

	if dryRun {
		log.Printf("Would update sudoers to: %s", targetLine)
		return nil
	}

	// Read current sudoers file
	content, err := os.ReadFile(SUDOERS_PATH)
	if err != nil {
		return fmt.Errorf("reading sudoers file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	found := false

	// Look for our managed line or the user's sudo line
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is our managed line
		if strings.Contains(line, SUDOERS_MARKER) {
			// Replace with the target line
			newLines = append(newLines, targetLine+" "+SUDOERS_MARKER)
			found = true
			continue
		}

		// Check if this is an unmanaged line for our user
		if strings.HasPrefix(trimmed, config.Sudoers.User+" ") ||
			strings.HasPrefix(trimmed, "# "+config.Sudoers.User+" ") {
			// Replace with our managed version
			newLines = append(newLines, targetLine+" "+SUDOERS_MARKER)
			found = true
			continue
		}

		newLines = append(newLines, line)
	}

	// If we didn't find any line, add it at the end
	if !found {
		newLines = append(newLines, targetLine+" "+SUDOERS_MARKER)
	}

	newContent := strings.Join(newLines, "\n")

	// Write to a temporary file
	tmpFile := SUDOERS_PATH + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(newContent), 0440); err != nil {
		return fmt.Errorf("writing temporary sudoers file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Validate the file with visudo
	cmd := exec.Command("visudo", "-c", "-f", tmpFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudoers validation failed: %w", err)
	}

	// Validation passed, now replace the real file
	if err := os.Rename(tmpFile, SUDOERS_PATH); err != nil {
		return fmt.Errorf("replacing sudoers file: %w", err)
	}

	// Ensure correct permissions
	if err := os.Chmod(SUDOERS_PATH, 0440); err != nil {
		log.Printf("Warning: couldn't set sudoers permissions: %v", err)
	}

	log.Printf("✓ Updated sudoers: %s", targetLine)
	return nil
}

func isSudoAllowed(config *Config, now time.Time) bool {
	if !config.Sudoers.Enabled {
		return true // If not enabled, don't restrict
	}

	currentDay := now.Weekday().String()[:3]
	currentTime := now.Format("15:04")

	// Check if current time falls within any allowed window
	for _, window := range config.Sudoers.TimeAllowed {
		if !slices.Contains(window.Days, currentDay) {
			continue
		}

		if isInTimeWindow(currentTime, window.Start, window.End) {
			return true
		}
	}

	return false
}

func createSudoersBackup() error {
	// Check if backup already exists
	if _, err := os.Stat(SUDOERS_BACKUP); err == nil {
		// Backup already exists, don't overwrite
		return nil
	}

	// Read current sudoers
	content, err := os.ReadFile(SUDOERS_PATH)
	if err != nil {
		return err
	}

	// Write backup
	return os.WriteFile(SUDOERS_BACKUP, content, 0440)
}

func getDomainsToBlock(config *Config, now time.Time) []string {
	var blocked []string
	currentDay := now.Weekday().String()[:3] // Mon, Tue, etc.
	currentTime := now.Format("15:04")

	for _, domain := range config.Domains {
		if domain.AlwaysBlock {
			blocked = append(blocked, domain.Name)
			continue
		}

		// Check time windows
		for _, window := range domain.TimeWindows {
			if !contains(window.Days, currentDay) {
				continue
			}

			if isInTimeWindow(currentTime, window.Start, window.End) {
				blocked = append(blocked, domain.Name)
				break
			}
		}
	}

	return blocked
}

func updateHosts(config *Config, domains []string, dryRun bool) error {
	hostsPath := config.HostsPath

	log.Printf("Updating %s with %d domains", hostsPath, len(domains))

	// Read current hosts file
	content, err := os.ReadFile(hostsPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading hosts file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	inBlockSection := false

	// Remove old block section
	for _, line := range lines {
		if strings.Contains(line, HOSTS_MARKER_START) {
			inBlockSection = true
			continue
		}
		if strings.Contains(line, HOSTS_MARKER_END) {
			inBlockSection = false
			continue
		}
		if !inBlockSection {
			newLines = append(newLines, line)
		}
	}

	// Add new block section
	blockSection := []string{HOSTS_MARKER_START}
	for _, domain := range domains {
		blockSection = append(blockSection,
			fmt.Sprintf("127.0.0.1 %s", domain),
			fmt.Sprintf("127.0.0.1 www.%s", domain),
			fmt.Sprintf("::1 %s", domain),
			fmt.Sprintf("::1 www.%s", domain),
		)
	}
	blockSection = append(blockSection, HOSTS_MARKER_END)

	// Combine
	newContent := strings.Join(newLines, "\n") + "\n" + strings.Join(blockSection, "\n") + "\n"

	if dryRun {
		log.Printf("Would write %d blocked domains to %s", len(domains), hostsPath)
		return nil
	}

	// Remove immutable flag temporarily
	exec.Command("chattr", "-i", hostsPath).Run()

	// Write file
	if err := os.WriteFile(hostsPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("writing hosts file: %w", err)
	}

	// Set immutable flag
	if err := exec.Command("chattr", "+i", hostsPath).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable flag on hosts: %v", err)
	}

	log.Printf("✓ Updated hosts file with %d domains", len(domains))
	return nil
}

func updateFirewall(domains []string, dryRun bool) error {
	log.Printf("Updating firewall rules for %d domains", len(domains))

	if dryRun {
		log.Printf("Would resolve and block IPs for %d domains", len(domains))
		return nil
	}

	// Clear old rules with our marker
	clearCmd := `iptables -S OUTPUT | grep 'DISTRACTION-BLOCK' | sed 's/-A/-D/' | while read rule; do iptables $rule 2>/dev/null; done`
	exec.Command("bash", "-c", clearCmd).Run()

	// Also clear ip6tables rules
	clearCmd6 := `ip6tables -S OUTPUT | grep 'DISTRACTION-BLOCK' | sed 's/-A/-D/' | while read rule; do ip6tables $rule 2>/dev/null; done`
	exec.Command("bash", "-c", clearCmd6).Run()

	totalIPs := 0
	for _, domain := range domains {
		// Resolve and block IPv4 addresses
		ips := resolveIPs(domain, "A")
		for _, ip := range ips {
			cmd := exec.Command("iptables", "-I", "OUTPUT", "-d", ip,
				"-j", "REJECT", "--reject-with", "icmp-host-unreachable",
				"-m", "comment", "--comment", "DISTRACTION-BLOCK")

			if err := cmd.Run(); err != nil {
				log.Printf("Warning: couldn't block IP %s for %s: %v", ip, domain, err)
			} else {
				totalIPs++
			}
		}

		// Resolve and block IPv6 addresses
		ips6 := resolveIPs(domain, "AAAA")
		for _, ip := range ips6 {
			cmd := exec.Command("ip6tables", "-I", "OUTPUT", "-d", ip,
				"-j", "REJECT", "--reject-with", "icmp6-adm-prohibited",
				"-m", "comment", "--comment", "DISTRACTION-BLOCK")

			if err := cmd.Run(); err != nil {
				log.Printf("Warning: couldn't block IPv6 %s for %s: %v", ip, domain, err)
			} else {
				totalIPs++
			}
		}
	}

	log.Printf("✓ Updated firewall: blocked %d IP addresses for %d domains", totalIPs, len(domains))
	return nil
}

func resolveIPs(domain string, recordType string) []string {
	var ips []string

	// Try to resolve the domain
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

func showStatus(config *Config) {
	now := time.Now()
	blocked := getDomainsToBlock(config, now)
	sudoAllowed := isSudoAllowed(config, now)

	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║     DISTRACTION BLOCKER STATUS                 ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Printf("\n📅 Current time: %s\n", now.Format("Mon Jan 2, 2006 15:04:05"))

	fmt.Println("\n🔧 Components:")
	fmt.Printf("   Hosts file:    %s\n", statusEmoji(config.EnableHosts))
	fmt.Printf("   Firewall:      %s\n", statusEmoji(config.EnableFirewall))
	fmt.Printf("   Sudoers:       %s\n", statusEmoji(config.Sudoers.Enabled))
	fmt.Printf("   Self-healing:  %s\n", statusEmoji(config.SelfHeal))

	if config.Sudoers.Enabled {
		fmt.Printf("\n🔐 Sudo Access: %s\n", map[bool]string{
			true:  "✅ ALLOWED (no password)",
			false: "🔒 RESTRICTED (password required)",
		}[sudoAllowed])
	}

	fmt.Printf("\n🚫 Currently blocked (%d domains):\n", len(blocked))
	if len(blocked) == 0 {
		fmt.Println("   (none)")
	} else {
		for i, domain := range blocked {
			if i < 10 { // Show first 10
				fmt.Printf("   • %s\n", domain)
			} else if i == 10 {
				fmt.Printf("   ... and %d more\n", len(blocked)-10)
				break
			}
		}
	}

	fmt.Printf("\n📋 All configured domains (%d):\n", len(config.Domains))
	for i, domain := range config.Domains {
		if i < 15 {
			status := "⏰ scheduled"
			if domain.AlwaysBlock {
				status = "🔒 always"
			}
			fmt.Printf("   • %-30s %s\n", domain.Name, status)
		} else if i == 15 {
			fmt.Printf("   ... and %d more\n", len(config.Domains)-15)
			break
		}
	}

	// Check if binary is protected
	if _, err := os.Stat(INSTALL_PATH); err == nil {
		cmd := exec.Command("lsattr", INSTALL_PATH)
		if output, err := cmd.Output(); err == nil {
			if strings.Contains(string(output), "i") {
				fmt.Println("\n🛡️  Binary protection: ACTIVE (immutable)")
			} else {
				fmt.Println("\n⚠️  Binary protection: INACTIVE (not immutable)")
				fmt.Println("   Run with -install flag to enable protection")
			}
		}

		// Check setuid bit
		if info, err := os.Stat(INSTALL_PATH); err == nil {
			mode := info.Mode()
			if mode&os.ModeSetuid != 0 {
				fmt.Println("🛡️  Setuid bit: ACTIVE (runs as root)")
			} else {
				fmt.Println("⚠️  Setuid bit: INACTIVE")
				fmt.Println("   Run with -install flag to enable setuid")
			}
		}
	}

	fmt.Println("\n" + strings.Repeat("─", 50))
}

func statusEmoji(enabled bool) string {
	if enabled {
		return "✅ enabled"
	}
	return "❌ disabled"
}

func validateConfig(config *Config) error {
	for _, domain := range config.Domains {
		if domain.Name == "" {
			return fmt.Errorf("domain name cannot be empty")
		}
		for _, window := range domain.TimeWindows {
			if !isValidTime(window.Start) || !isValidTime(window.End) {
				return fmt.Errorf("invalid time format for domain %s (use HH:MM)", domain.Name)
			}
			if len(window.Days) == 0 {
				return fmt.Errorf("time window for %s must specify at least one day", domain.Name)
			}
		}
	}

	// Validate sudoers config
	if config.Sudoers.Enabled {
		if config.Sudoers.User == "" {
			return fmt.Errorf("sudoers.user cannot be empty when sudoers is enabled")
		}
		if config.Sudoers.AllowedSudoersLine == "" {
			return fmt.Errorf("sudoers.allowed_sudoers_line cannot be empty when sudoers is enabled")
		}
		if config.Sudoers.BlockedSudoersLine == "" {
			return fmt.Errorf("sudoers.blocked_sudoers_line cannot be empty when sudoers is enabled")
		}
		for _, window := range config.Sudoers.TimeAllowed {
			if !isValidTime(window.Start) || !isValidTime(window.End) {
				return fmt.Errorf("invalid time format in sudoers time_allowed (use HH:MM)")
			}
			if len(window.Days) == 0 {
				return fmt.Errorf("sudoers time_allowed window must specify at least one day")
			}
		}
	}

	return nil
}

func isValidTime(timeStr string) bool {
	_, err := time.Parse("15:04", timeStr)
	return err == nil
}

func isInTimeWindow(current, start, end string) bool {
	// Simple string comparison works for HH:MM format
	if start <= end {
		// Normal case: 09:00 - 17:00
		return current >= start && current <= end
	}
	// Wraparound case: 22:00 - 02:00
	return current >= start || current <= end
}

// Additional security: make this process harder to kill
func init() {
	// Set process priority to make it less likely to be killed by OOM
	syscall.Setpriority(syscall.PRIO_PROCESS, 0, -10)
}
