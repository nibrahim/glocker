package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mailgun/mailgun-go/v4"
	"gopkg.in/yaml.v3"
)

const (
	INSTALL_PATH        = "/usr/local/bin/glocker"
	GLOCKER_CONFIG_FILE = "/etc/glocker/config.yaml"
	HOSTS_MARKER_START  = "### GLOCKER START ###"
	SUDOERS_PATH        = "/etc/sudoers"
	SUDOERS_BACKUP      = "/etc/sudoers.glocker.backup"
	SUDOERS_MARKER      = "# GLOCKER-MANAGED"
	SYSTEMD_FILE        = "./extras/glocker.service"
	GLOCKER_SOCK        = "/tmp/glocker.sock"
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
	LogBlocking bool         `yaml:"log_blocking,omitempty"`
	Absolute    bool         `yaml:"absolute,omitempty"`
}

type SudoersConfig struct {
	Enabled            bool         `yaml:"enabled"`
	User               string       `yaml:"user"`
	AllowedSudoersLine string       `yaml:"allowed_sudoers_line"`
	BlockedSudoersLine string       `yaml:"blocked_sudoers_line"`
	TimeAllowed        []TimeWindow `yaml:"time_allowed"`
}

type AccountabilityConfig struct {
	Enabled            bool   `yaml:"enabled"`
	PartnerEmail       string `yaml:"partner_email"`
	FromEmail          string `yaml:"from_email"`
	ApiKey             string `yaml:"api_key"`
	DailyReportTime    string `yaml:"daily_report_time"`
	DailyReportEnabled bool   `yaml:"daily_report_enabled"`
}

type TamperConfig struct {
	Enabled       bool   `yaml:"enabled"`
	CheckInterval int    `yaml:"check_interval_seconds"`
	AlarmCommand  string `yaml:"alarm_command"`
}

type WebTrackingConfig struct {
	Enabled bool   `yaml:"enabled"`
	Command string `yaml:"command"`
}

type ContentMonitoringConfig struct {
	Enabled bool   `yaml:"enabled"`
	LogFile string `yaml:"log_file"`
}

type ExtensionKeywordsConfig struct {
	URLKeywords     []string `yaml:"url_keywords"`
	ContentKeywords []string `yaml:"content_keywords"`
	Whitelist       []string `yaml:"whitelist"`
}

type ViolationTrackingConfig struct {
	Enabled           bool   `yaml:"enabled"`
	MaxViolations     int    `yaml:"max_violations"`
	TimeWindowMinutes int    `yaml:"time_window_minutes"`
	Command           string `yaml:"command"`
	ResetDaily        bool   `yaml:"reset_daily"`
	ResetTime         string `yaml:"reset_time"`
}

type UnblockingConfig struct {
	Reasons []string `yaml:"reasons"`
	LogFile string   `yaml:"log_file"`
}

type ForbiddenProgram struct {
	Name        string       `yaml:"name"`
	TimeWindows []TimeWindow `yaml:"time_windows"`
}

type ForbiddenProgramsConfig struct {
	Enabled       bool               `yaml:"enabled"`
	CheckInterval int                `yaml:"check_interval_seconds"`
	Programs      []ForbiddenProgram `yaml:"programs"`
}

type Config struct {
	EnableHosts             bool                    `yaml:"enable_hosts"`
	EnableFirewall          bool                    `yaml:"enable_firewall"`
	EnableForbiddenPrograms bool                    `yaml:"enable_forbidden_programs"`
	Domains                 []Domain                `yaml:"domains"`
	HostsPath               string                  `yaml:"hosts_path"`
	SelfHeal                bool                    `yaml:"enable_self_healing"`
	EnforceInterval         int                     `yaml:"enforce_interval_seconds"`
	Sudoers                 SudoersConfig           `yaml:"sudoers"`
	TamperDetection         TamperConfig            `yaml:"tamper_detection"`
	Accountability          AccountabilityConfig    `yaml:"accountability"`
	WebTracking             WebTrackingConfig       `yaml:"web_tracking"`
	ContentMonitoring       ContentMonitoringConfig `yaml:"content_monitoring"`
	ForbiddenPrograms       ForbiddenProgramsConfig `yaml:"forbidden_programs"`
	ExtensionKeywords       ExtensionKeywordsConfig `yaml:"extension_keywords"`
	ViolationTracking       ViolationTrackingConfig `yaml:"violation_tracking"`
	Unblocking              UnblockingConfig        `yaml:"unblocking"`
	MindfulDelay            int                     `yaml:"mindful_delay"`
	TempUnblockTime         int                     `yaml:"temp_unblock_time"`
	NotificationCommand     string                  `yaml:"notification_command"`
	Dev                     bool                    `yaml:"dev"`
	LogLevel                string                  `yaml:"log_level"`
}

func loadConfig() (Config, error) {
	var config Config

	// Read from external config file
	if _, err := os.Stat(GLOCKER_CONFIG_FILE); err != nil {
		if os.IsNotExist(err) {
			return config, fmt.Errorf("config file not found at %s\n\nThis usually means glocker is not properly installed.\nPlease check:\n  1. Is glocker installed? Run: ls -la %s\n  2. Is the glocker service running? Run: systemctl status glocker.service\n  3. If not installed, run: sudo glocker -install\n\nOriginal error: %w", GLOCKER_CONFIG_FILE, INSTALL_PATH, err)
		}
		return config, fmt.Errorf("config file access error at %s: %w", GLOCKER_CONFIG_FILE, err)
	}

	slog.Debug("Loading config from external file", "path", GLOCKER_CONFIG_FILE)
	configData, err := os.ReadFile(GLOCKER_CONFIG_FILE)
	if err != nil {
		return config, fmt.Errorf("reading config file: %w", err)
	}
	if err := yaml.Unmarshal(configData, &config); err != nil {
		return config, fmt.Errorf("parsing config file: %w", err)
	}

	return config, nil
}

func setupLogging(config *Config) {
	var level slog.Level

	switch strings.ToLower(config.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewTextHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Debug("Logging initialized", "level", level.String())
}

func main() {
	status := flag.Bool("status", false, "Show current status and configuration")
	enforce := flag.Bool("enforce", false, "Run enforcement loop (runs continuously)")
	once := flag.Bool("once", false, "Run enforcement once and exit")
	install := flag.Bool("install", false, "Install Glocker")
	uninstall := flag.String("uninstall", "", "Uninstall Glocker and revert all changes (provide reason)")
	reload := flag.Bool("reload", false, "Reload configuration from config file")
	blockHosts := flag.String("block", "", "Comma-separated list of hosts to add to always block list")
	unblockHosts := flag.String("unblock", "", "Comma-separated list of hosts to temporarily unblock (format: 'domain1,domain2:reason')")
	addKeyword := flag.String("add-keyword", "", "Comma-separated list of keywords to add to both URL and content keyword lists")
	flag.Parse()

	if *install {
		if !runningAsRoot(false) {
			log.Fatal("Program should run as root for installation.")
		}
		installGlocker()
		return
	}

	// Load config from external file
	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logging
	setupLogging(&config)
	slog.Debug("Configuration parsed successfully")

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

	if *uninstall != "" {
		if !runningAsRoot(true) {
			log.Fatal("Program should run as root for uninstalling.")
		}
		// Check if glocker is actually installed
		if _, err := os.Stat(INSTALL_PATH); os.IsNotExist(err) {
			log.Fatal("Glocker is not installed. Nothing to uninstall.")
		}
		// Send uninstall request via socket and wait for completion
		handleUninstallRequest(*uninstall)
		return
	}

	if *reload {
		if !runningAsRoot(true) {
			log.Fatal("Program should run as root for reloading configuration.")
		}
		handleReloadRequest()
		return
	}

	if *blockHosts != "" {
		blockHostsFromFlag(&config, *blockHosts)
		return
	}

	if *unblockHosts != "" {
		unblockHostsFromFlag(&config, *unblockHosts)
		return
	}

	if *addKeyword != "" {
		addKeywordsFromFlag(&config, *addKeyword)
		return
	}

	if *status {
		handleStatusCommand(&config)
		return
	}

	if *once {
		runOnce(&config, false)
		return
	}

	if *enforce {
		log.Println("Starting enforcement loop...")
		log.Printf("Enforcement interval: %d seconds", config.EnforceInterval)

		// Set up communication channel for block/unblock requests
		setupCommunication(&config)

		// Start tamper detection in background if enabled
		if config.TamperDetection.Enabled {
			// Capture initial checksums before starting monitoring
			filesToMonitor := []string{
				INSTALL_PATH,
				GLOCKER_CONFIG_FILE,
				config.HostsPath,
				SUDOERS_PATH,
				"/etc/systemd/system/glocker.service",
			}

			var initialChecksums []FileChecksum
			for _, filePath := range filesToMonitor {
				checksum := captureChecksum(&config, filePath)
				initialChecksums = append(initialChecksums, checksum)
			}
			log.Println("Initial checksums:")
			for _, c := range initialChecksums {
				log.Println(c)
			}

			// Store global references for checksum updates
			globalChecksums = initialChecksums
			globalFilesToMonitor = filesToMonitor
			globalConfig = &config

			go monitorTampering(&config, initialChecksums, filesToMonitor)
		}

		// Start web tracking server if enabled (also handles content monitoring)
		if config.WebTracking.Enabled || config.ContentMonitoring.Enabled {
			go startWebTrackingServer(&config)
		}

		// Start forbidden programs monitoring if enabled
		if config.EnableForbiddenPrograms && config.ForbiddenPrograms.Enabled {
			go monitorForbiddenPrograms(&config)
		}

		// Start violation tracking if enabled
		if config.ViolationTracking.Enabled {
			go monitorViolations(&config)
		}

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

	// If no flags provided, check if glocker is running and show status or help
	if flag.NFlag() == 0 {
		// Check if socket exists and is accessible
		if _, err := os.Stat(GLOCKER_SOCK); err == nil {
			// Socket exists, try to connect and get live status
			conn, err := net.Dial("unix", GLOCKER_SOCK)
			if err == nil {
				defer conn.Close()

				log.Println("=== LIVE STATUS ===")

				// Send status request
				conn.Write([]byte("status\n"))

				// Read response
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "END" {
						break
					}
					fmt.Println(line)
				}
				return
			}
		}

		// Socket not available, show help
		fmt.Println("Glocker - Domain and System Access Control")
		fmt.Println()
		fmt.Println("Usage:")
		flag.PrintDefaults()
		return
	}
}

func setupCommunication(config *Config) {
	socketPath := GLOCKER_SOCK

	// Remove existing socket
	os.Remove(socketPath)

	// Create Unix domain socket
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("Failed to create socket: %v", err)
	}

	// Set permissions
	os.Chmod(socketPath, 0600)

	go handleSocketConnections(config, listener)
}

func handleSocketConnections(config *Config, listener net.Listener) {
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Socket accept error: %v", err)
			continue
		}

		go handleSocketConnection(config, conn)
	}
}

func handleSocketConnection(config *Config, conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 1 {
			conn.Write([]byte("ERROR: Invalid format\n"))
			continue
		}

		action := strings.TrimSpace(parts[0])

		switch action {
		case "status":
			response := getStatusResponse(config)
			conn.Write([]byte(response))
		case "reload":
			conn.Write([]byte("OK: Reload request received\n"))
			go processReloadRequest(config, conn)
		case "unblock":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'unblock:domains:reason'\n"))
				continue
			}
			payload := strings.TrimSpace(parts[1])
			// Split payload into domains and reason
			payloadParts := strings.SplitN(payload, ":", 2)
			if len(payloadParts) != 2 {
				conn.Write([]byte("ERROR: Reason required. Use 'unblock:domains:reason'\n"))
				continue
			}
			domains := strings.TrimSpace(payloadParts[0])
			reason := strings.TrimSpace(payloadParts[1])
			if reason == "" {
				conn.Write([]byte("ERROR: Reason cannot be empty\n"))
				continue
			}
			conn.Write([]byte("OK: Unblock request received\n"))
			go processUnblockRequestWithReason(config, domains, reason)
		case "block":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'block:domains'\n"))
				continue
			}
			domains := strings.TrimSpace(parts[1])
			conn.Write([]byte("OK: Block request received\n"))
			go processBlockRequest(config, domains)
		case "add-keyword":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'add-keyword:keywords'\n"))
				continue
			}
			keywords := strings.TrimSpace(parts[1])
			conn.Write([]byte("OK: Add keyword request received\n"))
			go processAddKeywordRequest(config, keywords)
		case "uninstall":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'uninstall:reason'\n"))
				continue
			}
			reason := strings.TrimSpace(parts[1])
			if reason == "" {
				conn.Write([]byte("ERROR: Reason cannot be empty\n"))
				continue
			}
			conn.Write([]byte("OK: Uninstall request received\n"))
			go processUninstallRequestWithCompletion(config, reason, conn)
		default:
			conn.Write([]byte("ERROR: Unknown action. Use 'block:domains', 'unblock:domains:reason', 'add-keyword:keywords', 'uninstall:reason', 'reload', or 'status'\n"))
		}
	}
}

func processUnblockRequestWithReason(config *Config, hostsStr string, reason string) {
	slog.Debug("Processing unblock request", "hosts_string", hostsStr, "reason", reason)

	// First, validate the reason
	if !isValidUnblockReason(config, reason) {
		log.Printf("UNBLOCK REJECTED: Invalid reason '%s'. Valid reasons: %v", reason, config.Unblocking.Reasons)
		return
	}

	hosts := strings.Split(hostsStr, ",")
	var validHosts []string
	var rejectedHosts []string

	// Clean, validate hosts, and check for absolute domains
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host != "" {
			// Check if this domain is marked as absolute
			isAbsolute := false
			for _, domain := range config.Domains {
				if domain.Name == host && domain.Absolute {
					isAbsolute = true
					break
				}
			}

			if isAbsolute {
				rejectedHosts = append(rejectedHosts, host)
				slog.Debug("Rejected absolute domain for unblocking", "host", host)
			} else {
				validHosts = append(validHosts, host)
			}
		}
	}

	if len(rejectedHosts) > 0 {
		log.Printf("Cannot unblock absolute domains: %v", rejectedHosts)
	}

	if len(validHosts) == 0 {
		slog.Debug("No valid hosts provided for unblocking (all rejected or empty)")
		if len(rejectedHosts) > 0 {
			log.Println("All requested domains are marked as absolute and cannot be temporarily unblocked.")
		}
		return
	}

	// Set temporary unblock time (default 30 minutes)
	unblockDuration := config.TempUnblockTime
	if unblockDuration == 0 {
		unblockDuration = 30
	}

	expiresAt := time.Now().Add(time.Duration(unblockDuration) * time.Minute)
	slog.Debug("Temporary unblock configuration", "duration_minutes", unblockDuration, "expires_at", expiresAt.Format("2006-01-02 15:04:05"))

	// Log all unblocked hosts since these are manual actions
	log.Printf("Temporarily unblocking %d hosts for %d minutes: %v (Reason: %s)", len(validHosts), unblockDuration, validHosts, reason)
	if len(rejectedHosts) > 0 {
		log.Printf("Rejected %d absolute domains that cannot be unblocked: %v", len(rejectedHosts), rejectedHosts)
	}

	// Add to temporary unblock list
	for _, host := range validHosts {
		tempUnblocks = append(tempUnblocks, TempUnblock{
			Domain:    host,
			ExpiresAt: expiresAt,
		})
		// Log each host being unblocked since it's a manual action
		slog.Debug("Added host to temporary unblock list", "host", host, "expires_at", expiresAt.Format("2006-01-02 15:04:05"))
	}

	// Apply the unblocking immediately
	runOnce(config, false)

	// Update checksum for hosts file after legitimate changes
	if globalConfig != nil {
		updateChecksum(config.HostsPath)
		log.Println("Updated checksum for hosts file after unblocking domains")
	}

	// Send desktop notification
	sendNotification(config, "Glocker Alert", 
		fmt.Sprintf("Temporarily unblocked %d domains for %d minutes", len(validHosts), unblockDuration),
		"normal", "dialog-information")

	// Send accountability email
	if config.Accountability.Enabled {
		subject := "GLOCKER ALERT: Domains Temporarily Unblocked"
		body := fmt.Sprintf("The following domains were temporarily unblocked at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
		for _, host := range validHosts {
			body += fmt.Sprintf("  - %s (expires at %s)\n", host, expiresAt.Format("2006-01-02 15:04:05"))
		}
		body += fmt.Sprintf("\nReason provided: %s\n", reason)
		body += fmt.Sprintf("\nThey will be automatically re-blocked after %d minutes.\n", unblockDuration)
		body += "\nThis is an automated alert from Glocker."

		if err := sendEmail(config, subject, body); err != nil {
			log.Printf("Failed to send accountability email: %v", err)
		}
	}

	// Send accountability email for rejected absolute domains
	if len(rejectedHosts) > 0 && config.Accountability.Enabled {
		var subject string
		var body string

		if len(validHosts) > 0 {
			// Partial rejection - some domains were unblocked
			subject = "GLOCKER ALERT: Unblock Request Partially Rejected"
			body = fmt.Sprintf("An unblock request was partially rejected at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
			body += fmt.Sprintf("Successfully unblocked domains: %v\n", validHosts)
			body += fmt.Sprintf("Rejected absolute domains: %v\n", rejectedHosts)
		} else {
			// Complete rejection - all domains were absolute
			subject = "GLOCKER ALERT: Unblock Request Completely Rejected"
			body = fmt.Sprintf("An unblock request was completely rejected at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
			body += fmt.Sprintf("All requested domains are marked as absolute: %v\n", rejectedHosts)
		}

		body += fmt.Sprintf("Reason provided: %s\n\n", reason)
		body += "Absolute domains cannot be temporarily unblocked due to their configuration.\n"
		body += "\nThis is an automated alert from Glocker."

		if err := sendEmail(config, subject, body); err != nil {
			log.Printf("Failed to send accountability email for rejected domains: %v", err)
		}
	}
}

func processBlockRequest(config *Config, hostsStr string) {
	slog.Debug("Processing block request", "hosts_string", hostsStr)

	hosts := strings.Split(hostsStr, ",")
	var validHosts []string

	// Clean and validate hosts
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host != "" {
			validHosts = append(validHosts, host)
			slog.Debug("Added valid host for blocking", "host", host)
		}
	}

	if len(validHosts) == 0 {
		slog.Debug("No valid hosts provided for blocking")
		return
	}

	slog.Debug("Valid hosts for blocking", "count", len(validHosts), "hosts", validHosts)
	log.Printf("Adding %d hosts to always block list: %v", len(validHosts), validHosts)

	// Add hosts to config as always blocked domains
	for _, host := range validHosts {
		slog.Debug("Processing host for permanent blocking", "host", host)

		// Check if domain already exists
		found := false
		for i, domain := range config.Domains {
			if domain.Name == host {
				// Update existing domain to always block
				slog.Debug("Found existing domain, updating to always block", "host", host, "was_always_block", domain.AlwaysBlock)
				config.Domains[i].AlwaysBlock = true
				config.Domains[i].TimeWindows = nil  // Clear time windows since it's always blocked
				config.Domains[i].LogBlocking = true // Enable logging for manually blocked domains
				found = true
				log.Printf("Updated existing domain %s to always block", host)
				break
			}
		}

		if !found {
			// Add new domain
			newDomain := Domain{
				Name:        host,
				AlwaysBlock: true,
				LogBlocking: true, // Enable logging for manually blocked domains
			}
			config.Domains = append(config.Domains, newDomain)
			slog.Debug("Added new domain to always block", "host", host)
			log.Printf("Added new domain %s to always block", host)
		}
	}

	// Send desktop notification
	sendNotification(config, "Glocker Alert", 
		fmt.Sprintf("Added %d domains to block list", len(validHosts)),
		"normal", "dialog-information")

	// Apply the blocking immediately
	log.Println("Applying blocks...")
	runOnce(config, false)

	// Update checksum for hosts file after legitimate changes
	if globalConfig != nil {
		updateChecksum(config.HostsPath)
		log.Println("Updated checksum for hosts file after blocking new domains")
	}

	// Broadcast keyword update to connected browser extensions
	broadcastKeywordUpdate(config)

	log.Println("Hosts have been blocked successfully!")
}

func runOnce(config *Config, dryRun bool) {
	now := time.Now()
	slog.Debug("Starting enforcement run", "time", now.Format("2006-01-02 15:04:05"), "dry_run", dryRun)

	// Clean up expired temporary unblocks
	cleanupExpiredUnblocks(now)

	blockedDomains := getDomainsToBlock(config, now)
	slog.Debug("Domains to block determined", "count", len(blockedDomains))

	// Self-healing: verify our own integrity
	if config.SelfHeal && !dryRun {
		slog.Debug("Running self-healing checks")
		selfHeal()
	}

	if config.EnableHosts {
		slog.Debug("Updating hosts file", "enabled", true)
		if err := updateHosts(config, blockedDomains, dryRun); err != nil {
			log.Printf("ERROR updating hosts: %v", err)
		}
	} else {
		slog.Debug("Hosts file management disabled")
	}

	if config.EnableFirewall {
		slog.Debug("Updating firewall rules", "enabled", true)
		if err := updateFirewall(blockedDomains, dryRun); err != nil {
			log.Printf("ERROR updating firewall: %v", err)
		}
	} else {
		slog.Debug("Firewall management disabled")
	}

	if config.Sudoers.Enabled {
		slog.Debug("Updating sudoers configuration", "enabled", true)
		if err := updateSudoers(config, now, dryRun); err != nil {
			log.Printf("ERROR updating sudoers: %v", err)
		}
	} else {
		slog.Debug("Sudoers management disabled")
	}

	if config.EnableForbiddenPrograms && config.ForbiddenPrograms.Enabled {
		slog.Debug("Forbidden programs monitoring", "enabled", true)
		// Note: Forbidden programs are monitored in a separate goroutine
		// This is just for status reporting
	} else {
		slog.Debug("Forbidden programs monitoring disabled")
	}

	slog.Debug("Enforcement run completed")
}

func selfHeal() {
	// Check if our binary still exists
	if _, err := os.Stat(INSTALL_PATH); os.IsNotExist(err) {
		log.Fatal("CRITICAL: glocker binary was deleted! Self-healing failed.")
	}

	// Re-apply immutable flag on our binary
	exec.Command("chattr", "+i", INSTALL_PATH).Run()

	// Verify we're still running as the expected process
	exe, err := os.Executable()
	if err == nil {
		exePath, _ := filepath.EvalSymlinks(exe)
		if exePath != INSTALL_PATH {
			log.Printf("Warning: running from unexpected location: %s (expected %s)", exePath, INSTALL_PATH)
		}
	}
}

func installGlocker() {
	log.Println("╔════════════════════════════════════════════════╗")
	log.Println("║              GLOCKER FULL INSTALL              ║")
	log.Println("╚════════════════════════════════════════════════╝")
	log.Println()

	// Step 1: Get current executable path
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("Failed to get executable path: %v", err)
	}

	exePath, err := filepath.EvalSymlinks(exe)
	if err != nil {
		log.Fatalf("Failed to resolve executable path: %v", err)
	}

	// Copy config file from conf/conf.yaml to target location
	log.Printf("Copying config file from conf/conf.yaml to %s", GLOCKER_CONFIG_FILE)

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(GLOCKER_CONFIG_FILE)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Fatalf("Failed to create config directory: %v", err)
	}

	// Copy the config file
	if err := copyFile("conf/conf.yaml", GLOCKER_CONFIG_FILE); err != nil {
		log.Fatalf("Failed to copy config file: %v", err)
	}
	log.Printf("✓ Config file copied to %s", GLOCKER_CONFIG_FILE)

	// Set ownership and make config file immutable
	if err := os.Chown(GLOCKER_CONFIG_FILE, 0, 0); err != nil {
		log.Fatalf("Failed to set config file ownership: %v", err)
	}
	if err := exec.Command("chattr", "+i", GLOCKER_CONFIG_FILE).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable flag on config file: %v", err)
	}

	// Copy binary to install location
	if err := copyFile(exePath, INSTALL_PATH); err != nil {
		log.Fatalf("Failed to copy binary: %v", err)
	}
	// Set ownership to root:root
	if err := os.Chown(INSTALL_PATH, 0, 0); err != nil {
		log.Fatalf("Warning: couldn't set ownership to root: %v", err)
	}
	// Set setuid bit (4755 = rwsr-xr-x)
	if err := os.Chmod(INSTALL_PATH, 0o755|os.ModeSetuid|os.ModeSetgid); err != nil {
		log.Fatalf("Failed to set setuid bit: %v", err)
	}

	// Set immutable on the installed binary
	if err := exec.Command("chattr", "+i", INSTALL_PATH).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable flag: %v", err)
	}

	// Install systemd service
	servicePath := "/etc/systemd/system/glocker.service"
	err = copyFile(SYSTEMD_FILE, servicePath)
	if err != nil {
		log.Fatalf("Failed to create service file: %v", err)
	}

	// Reload systemd daemon
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		log.Fatalf("Failed to reload systemd: %v", err)
	}

	// Enable service
	if err := exec.Command("systemctl", "enable", "glocker.service").Run(); err != nil {
		log.Fatalf("Failed to enable service: %v", err)
	}

	// Start service
	if err := exec.Command("systemctl", "start", "glocker.service").Run(); err != nil {
		log.Fatalf("Failed to start service: %v", err)
	}

	// Protect service file
	exec.Command("chattr", "+i", servicePath).Run()

	log.Println("Installation complete!")
}

func runningAsRoot(real bool) bool {
	var uid int
	if real {
		uid = os.Getuid() // Real user ID - who actually ran the command
	} else {
		uid = os.Geteuid() // Effective user ID - current privileges (affected by setuid)
	}
	return uid == 0
}

func runningWithSudo() bool {
	// Check if SUDO_USER environment variable is set
	// This is set by sudo when a command is run with sudo
	return os.Getenv("SUDO_USER") != ""
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	if err != nil {
		return err
	}

	// Copy file permissions
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.Chmod(dst, sourceInfo.Mode())
}

func mindfulDelay(config *Config) {
	// Skip mindful delay in dev mode
	if config.Dev {
		fmt.Println("DEV MODE: Skipping mindful delay")
		return
	}

	// Mixed Shakespeare and Sherlock Holmes quotes for mindful typing
	quotes := []string{
		"To be, or not to be, that is the question: Whether 'tis nobler in the mind to suffer the slings and arrows of outrageous fortune, or to take arms against a sea of troubles and by opposing end them.",
		"The game is afoot.",
		"All the world's a stage, and all the men and women merely players: they have their exits and their entrances; and one man in his time plays many parts, his acts being seven ages.",
		"Elementary, my dear Watson.",
		"What's in a name? That which we call a rose by any other name would smell as sweet.",
		"When you have eliminated the impossible, whatever remains, however improbable, must be the truth.",
		"The fault, dear Brutus, is not in our stars, but in ourselves, that we are underlings.",
		"I never guess. It is a capital mistake to theorize before one has data.",
		"Friends, Romans, countrymen, lend me your ears; I come to bury Caesar, not to praise him.",
		"You see, but you do not observe.",
		"Now is the winter of our discontent made glorious summer by this sun of York.",
		"The world is full of obvious things which nobody by any chance ever observes.",
		"If music be the food of love, play on; give me excess of it, that, surfeiting, the appetite may sicken, and so die.",
		"There is nothing more deceptive than an obvious fact.",
		"Double, double toil and trouble; fire burn and caldron bubble.",
		"I am not the law, but I represent justice so far as my feeble powers go.",
		"Out, out, brief candle! Life's but a walking shadow, a poor player that struts and frets his hour upon the stage and then is heard no more.",
		"My mind rebels at stagnation. Give me problems, give me work, give me the most abstruse cryptogram.",
		"Tomorrow, and tomorrow, and tomorrow, creeps in this petty pace from day to day to the last syllable of recorded time.",
		"The little things are infinitely the most important.",
		"Is this a dagger which I see before me, the handle toward my hand? Come, let me clutch thee.",
		"Violence does, in truth, recoil upon the violent, and the schemer falls into the pit which he digs for another.",
		"We are such stuff as dreams are made on, and our little life is rounded with a sleep.",
		"Education never ends, Watson. It is a series of lessons, with the greatest for the last.",
		"Lord, what fools these mortals be!",
		"There is nothing new under the sun. It has all been done before.",
		"The course of true love never did run smooth.",
		"Work is the best antidote to sorrow, my dear Watson.",
		"Cowards die many times before their deaths; the valiant never taste of death but once.",
		"The temptation to form premature theories upon insufficient data is the bane of our profession.",
		"Neither a borrower nor a lender be; for loan oft loses both itself and friend.",
		"Art in the blood is liable to take the strangest forms.",
		"This above all: to thine own self be true, and it must follow, as the night the day, thou canst not then be false to any man.",
		"Mediocrity knows nothing higher than itself; but talent instantly recognizes genius.",
		"A man should keep his little brain attic stocked with all the furniture that he is likely to use.",
	}

	// Select a random quote
	quote := quotes[time.Now().Unix()%int64(len(quotes))]

	fmt.Println("Please type the following quote EXACTLY as shown (including punctuation and capitalization):")
	fmt.Println()
	fmt.Println("Quote:")

	// Print quote with two words per line to prevent copy-paste
	words := strings.Fields(quote)
	for i := 0; i < len(words); i += 2 {
		if i+1 < len(words) {
			fmt.Printf("%s %s\n", words[i], words[i+1])
		} else {
			fmt.Printf("%s\n", words[i])
		}
	}

	fmt.Println()
	fmt.Print("Type here: ")

	// Read user input
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := scanner.Text()

	// Keep asking until they get it right
	for input != quote {
		fmt.Println()
		fmt.Println("❌ That doesn't match exactly. Please try again.")
		fmt.Println()
		fmt.Println("Quote:")

		// Print quote with two words per line to prevent copy-paste
		words := strings.Fields(quote)
		for i := 0; i < len(words); i += 2 {
			if i+1 < len(words) {
				fmt.Printf("%s %s\n", words[i], words[i+1])
			} else {
				fmt.Printf("%s\n", words[i])
			}
		}

		fmt.Println()
		fmt.Print("Type here: ")
		scanner.Scan()
		input = scanner.Text()
	}

	// Get delay from config (default to 30 seconds if not set)
	delaySeconds := config.MindfulDelay
	if delaySeconds == 0 {
		delaySeconds = 30
	}

	fmt.Printf("\n✓ Quote verified! Waiting %d seconds before proceeding...\n", delaySeconds)

	for i := delaySeconds; i > 0; i-- {
		if i <= 10 || i%5 == 0 {
			fmt.Printf("Proceeding in %d seconds...\n", i)
		}
		time.Sleep(1 * time.Second)
	}

	fmt.Println("✓ Delay complete!")
}

func cleanupHostsFile(config *Config) error {
	hostsPath := config.HostsPath
	if hostsPath == "" {
		hostsPath = "/etc/hosts"
	}

	// Remove immutable flag
	exec.Command("chattr", "-i", hostsPath).Run()

	// Read current hosts file
	content, err := os.ReadFile(hostsPath)
	if err != nil {
		return fmt.Errorf("reading hosts file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string

	// Remove everything after glocker start marker
	for _, line := range lines {
		if strings.Contains(line, HOSTS_MARKER_START) {
			// Stop processing once we hit the start marker
			break
		}
		newLines = append(newLines, line)
	}

	// Write cleaned content
	newContent := strings.Join(newLines, "\n")
	return os.WriteFile(hostsPath, []byte(newContent), 0644)
}

func restoreSudoers(config *Config) error {
	// Check if backup exists
	if _, err := os.Stat(SUDOERS_BACKUP); os.IsNotExist(err) {
		// No backup exists, replace blocked line with allowed line
		return replaceBlockedWithAllowed(config)
	}

	// Restore from backup
	backupContent, err := os.ReadFile(SUDOERS_BACKUP)
	if err != nil {
		return fmt.Errorf("reading sudoers backup: %w", err)
	}

	// Write to temporary file for validation
	tmpFile := SUDOERS_PATH + ".tmp"
	if err := os.WriteFile(tmpFile, backupContent, 0440); err != nil {
		return fmt.Errorf("writing temporary sudoers file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Validate with visudo
	cmd := exec.Command("visudo", "-c", "-f", tmpFile)
	if err := cmd.Run(); err != nil {
		// If backup is invalid, fall back to replacing blocked with allowed
		log.Printf("Backup sudoers file is invalid, replacing blocked line with allowed instead")
		return replaceBlockedWithAllowed(config)
	}

	// Validation passed, restore the backup
	if err := os.Rename(tmpFile, SUDOERS_PATH); err != nil {
		return fmt.Errorf("restoring sudoers file: %w", err)
	}

	// Ensure correct permissions
	return os.Chmod(SUDOERS_PATH, 0440)
}

func replaceBlockedWithAllowed(config *Config) error {
	// Read current sudoers file
	content, err := os.ReadFile(SUDOERS_PATH)
	if err != nil {
		return fmt.Errorf("reading sudoers file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	found := false

	// Look for our managed line and replace blocked with allowed
	for _, line := range lines {
		if strings.Contains(line, SUDOERS_MARKER) {
			// Replace with allowed line
			newLines = append(newLines, config.Sudoers.AllowedSudoersLine+" "+SUDOERS_MARKER)
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	// If we didn't find our managed line, add the allowed line
	if !found {
		newLines = append(newLines, config.Sudoers.AllowedSudoersLine+" "+SUDOERS_MARKER)
	}

	newContent := strings.Join(newLines, "\n")

	// Write to temporary file for validation
	tmpFile := SUDOERS_PATH + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(newContent), 0440); err != nil {
		return fmt.Errorf("writing temporary sudoers file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Validate with visudo
	cmd := exec.Command("visudo", "-c", "-f", tmpFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudoers validation failed: %w", err)
	}

	// Validation passed, replace the real file
	if err := os.Rename(tmpFile, SUDOERS_PATH); err != nil {
		return fmt.Errorf("replacing sudoers file: %w", err)
	}

	// Ensure correct permissions
	return os.Chmod(SUDOERS_PATH, 0440)
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

	if dryRun {
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
	os.Chmod(SUDOERS_PATH, 0440)

	// Update checksum after legitimate change
	updateChecksum(SUDOERS_PATH)

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

func getBlockingReason(config *Config, domain string, now time.Time) string {
	currentDay := now.Weekday().String()[:3]
	currentTime := now.Format("15:04")

	// Find the domain in the config
	for _, configDomain := range config.Domains {
		if configDomain.Name == domain {
			if configDomain.AlwaysBlock {
				if configDomain.Absolute {
					return "always blocked (absolute - cannot be temporarily unblocked)"
				}
				return "always blocked"
			}

			// Check which time window is active
			for _, window := range configDomain.TimeWindows {
				if slices.Contains(window.Days, currentDay) && isInTimeWindow(currentTime, window.Start, window.End) {
					return fmt.Sprintf("time-based block (active %s-%s on %s)", window.Start, window.End, strings.Join(window.Days, ","))
				}
			}
		}
	}

	return "unknown blocking rule"
}

func getDomainsToBlock(config *Config, now time.Time) []string {
	var blocked []string
	var loggedBlocked []string
	currentDay := now.Weekday().String()[:3] // Mon, Tue, etc.
	currentTime := now.Format("15:04")

	alwaysBlockCount := 0
	timeBasedBlockCount := 0
	tempUnblockedCount := 0

	slog.Debug("Evaluating domains for blocking", "current_day", currentDay, "current_time", currentTime, "total_domains", len(config.Domains))

	for _, domain := range config.Domains {
		if domain.LogBlocking {
			slog.Debug("Evaluating domain", "domain", domain.Name, "always_block", domain.AlwaysBlock, "absolute", domain.Absolute)
		}

		// Check if domain is temporarily unblocked
		if isTempUnblocked(domain.Name, now) {
			tempUnblockedCount++
			if domain.LogBlocking {
				slog.Debug("Domain is temporarily unblocked", "domain", domain.Name)
				log.Printf("DOMAIN STATUS: %s -> temporarily unblocked (expires soon)", domain.Name)
			}
			continue
		}

		if domain.AlwaysBlock {
			alwaysBlockCount++
			blocked = append(blocked, domain.Name)
			if domain.LogBlocking {
				blockType := "always blocked"
				if domain.Absolute {
					blockType = "always blocked (absolute)"
				}
				slog.Debug("Domain marked for always block", "domain", domain.Name, "absolute", domain.Absolute)
				log.Printf("DOMAIN STATUS: %s -> %s", domain.Name, blockType)
				loggedBlocked = append(loggedBlocked, domain.Name)
			}
			continue
		}

		// Check time windows
		domainBlocked := false
		activeWindow := ""
		for _, window := range domain.TimeWindows {
			if domain.LogBlocking {
				slog.Debug("Checking time window", "domain", domain.Name, "window_days", window.Days, "window_start", window.Start, "window_end", window.End)
			}

			if !slices.Contains(window.Days, currentDay) {
				if domain.LogBlocking {
					slog.Debug("Current day not in window", "domain", domain.Name, "current_day", currentDay)
				}
				continue
			}

			if isInTimeWindow(currentTime, window.Start, window.End) {
				timeBasedBlockCount++
				blocked = append(blocked, domain.Name)
				domainBlocked = true
				activeWindow = fmt.Sprintf("%s-%s on %s", window.Start, window.End, strings.Join(window.Days, ","))
				if domain.LogBlocking {
					slog.Debug("Domain blocked by time window", "domain", domain.Name, "window", fmt.Sprintf("%s-%s", window.Start, window.End))
					log.Printf("DOMAIN STATUS: %s -> blocked by time window (%s)", domain.Name, activeWindow)
					loggedBlocked = append(loggedBlocked, domain.Name)
				}
				break
			}
		}

		if !domainBlocked && len(domain.TimeWindows) > 0 && domain.LogBlocking {
			slog.Debug("Domain not blocked by any time window", "domain", domain.Name)
			log.Printf("DOMAIN STATUS: %s -> not blocked (outside time windows)", domain.Name)
		}
	}

	// Log summary with counts only
	slog.Debug("Domain blocking evaluation complete",
		"total_blocked", len(blocked),
		"always_block_count", alwaysBlockCount,
		"time_based_block_count", timeBasedBlockCount,
		"temp_unblocked_count", tempUnblockedCount,
		"logged_domains_count", len(loggedBlocked))

	return blocked
}

func isTempUnblocked(domain string, now time.Time) bool {
	for _, unblock := range tempUnblocks {
		if unblock.Domain == domain {
			if now.Before(unblock.ExpiresAt) {
				// Always log temporary unblocks since they're manual actions
				slog.Debug("Domain is temporarily unblocked", "domain", domain, "expires_at", unblock.ExpiresAt.Format("2006-01-02 15:04:05"))
				return true
			} else {
				// Always log when temporary unblocks expire since they're manual actions
				slog.Debug("Domain temporary unblock has expired", "domain", domain, "expired_at", unblock.ExpiresAt.Format("2006-01-02 15:04:05"))
			}
		}
	}
	return false
}

// cleanupExpiredUnblocks removes expired temporary unblocks from the slice
func cleanupExpiredUnblocks(now time.Time) {
	var activeUnblocks []TempUnblock
	expiredCount := 0

	for _, unblock := range tempUnblocks {
		if now.Before(unblock.ExpiresAt) {
			activeUnblocks = append(activeUnblocks, unblock)
		} else {
			expiredCount++
			slog.Debug("Removing expired temporary unblock", "domain", unblock.Domain, "expired_at", unblock.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
	}

	if expiredCount > 0 {
		tempUnblocks = activeUnblocks
		slog.Debug("Cleaned up expired temporary unblocks", "removed_count", expiredCount, "remaining_count", len(tempUnblocks))
	}
}

func updateHosts(config *Config, domains []string, dryRun bool) error {
	hostsPath := config.HostsPath
	slog.Debug("Starting hosts file update", "hosts_path", hostsPath, "domains_count", len(domains), "dry_run", dryRun)

	// Log the domains being processed
	if len(domains) > 0 {
		slog.Debug("Number of domains to block", "domains", len(domains))
	} else {
		slog.Debug("No domains to block - will clear existing blocks")
	}

	// Check if hosts file exists and get current permissions
	fileInfo, err := os.Stat(hostsPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Debug("Failed to stat hosts file", "error", err)
		return fmt.Errorf("checking hosts file: %w", err)
	}
	if fileInfo != nil {
		slog.Debug("Hosts file info", "size", fileInfo.Size(), "mode", fileInfo.Mode(), "mod_time", fileInfo.ModTime())
	}

	// Read current hosts file
	content, err := os.ReadFile(hostsPath)
	if err != nil && !os.IsNotExist(err) {
		slog.Debug("Failed to read hosts file", "error", err)
		return fmt.Errorf("reading hosts file: %w", err)
	}
	slog.Debug("Read hosts file", "size_bytes", len(content), "exists", err == nil)

	lines := strings.Split(string(content), "\n")
	var originalLines []string
	inBlockSection := false
	removedLines := 0
	originalLineCount := len(lines)

	slog.Debug("Processing hosts file content", "original_lines", originalLineCount)

	// Remove old glocker block section (everything after start marker)
	for i, line := range lines {
		if strings.Contains(line, HOSTS_MARKER_START) {
			slog.Debug("Found glocker block start marker", "line_number", i+1)
			inBlockSection = true
			// Don't include the start marker line or anything after it
			break
		}
		originalLines = append(originalLines, line)
	}

	if inBlockSection {
		removedLines = originalLineCount - len(originalLines)
		slog.Debug("Removed old glocker block", "removed_lines", removedLines, "remaining_lines", len(originalLines))
	}

	if dryRun {
		slog.Debug("Dry run mode - would write hosts file with chunked approach", "original_lines", len(originalLines), "domains_to_add", len(domains))
		return nil
	}

	// Remove immutable flag temporarily
	slog.Debug("Removing immutable flag from hosts file", "command", "chattr -i "+hostsPath)
	if err := exec.Command("chattr", "-i", hostsPath).Run(); err != nil {
		slog.Debug("Failed to remove immutable flag (may not be set)", "error", err)
	} else {
		slog.Debug("Successfully removed immutable flag")
	}

	// Open file for writing
	file, err := os.OpenFile(hostsPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		slog.Debug("Failed to open hosts file for writing", "error", err, "path", hostsPath)
		return fmt.Errorf("opening hosts file for writing: %w", err)
	}
	defer file.Close()

	slog.Debug("Writing hosts file in chunks", "path", hostsPath)

	// Write original content first
	if len(originalLines) > 0 {
		// Remove any trailing empty lines from original content
		for len(originalLines) > 0 && strings.TrimSpace(originalLines[len(originalLines)-1]) == "" {
			originalLines = originalLines[:len(originalLines)-1]
		}

		originalContent := strings.Join(originalLines, "\n")
		if originalContent != "" {
			originalContent += "\n"
		}

		if _, err := file.WriteString(originalContent); err != nil {
			slog.Debug("Failed to write original content", "error", err)
			return fmt.Errorf("writing original content: %w", err)
		}
		slog.Debug("Wrote original hosts content", "lines", len(originalLines))
	}

	// Add empty line and start marker
	if _, err := file.WriteString("\n" + HOSTS_MARKER_START + "\n"); err != nil {
		slog.Debug("Failed to write start marker", "error", err)
		return fmt.Errorf("writing start marker: %w", err)
	}
	slog.Debug("Wrote glocker start marker")

	// Write domains in chunks
	const chunkSize = 1000
	totalDomains := len(domains)
	chunksWritten := 0

	for i := 0; i < totalDomains; i += chunkSize {
		end := i + chunkSize
		if end > totalDomains {
			end = totalDomains
		}

		// Build chunk content
		var chunkBuilder strings.Builder
		for j := i; j < end; j++ {
			domain := domains[j]
			chunkBuilder.WriteString(fmt.Sprintf("127.0.0.1 %s\n", domain))
			chunkBuilder.WriteString(fmt.Sprintf("127.0.0.1 www.%s\n", domain))
			chunkBuilder.WriteString(fmt.Sprintf("::1 %s\n", domain))
			chunkBuilder.WriteString(fmt.Sprintf("::1 www.%s\n", domain))
		}

		// Write chunk to file
		if _, err := file.WriteString(chunkBuilder.String()); err != nil {
			slog.Debug("Failed to write domain chunk", "error", err, "chunk", chunksWritten+1)
			return fmt.Errorf("writing domain chunk %d: %w", chunksWritten+1, err)
		}

		chunksWritten++

		// Flush and log progress every 1000 chunks or at the end
		if chunksWritten%1000 == 0 || end == totalDomains {
			if err := file.Sync(); err != nil {
				slog.Debug("Failed to sync file", "error", err)
			}

			domainsProcessed := end
			log.Printf("Hosts file progress: %d/%d domains written (%d chunks, %.1f%%)",
				domainsProcessed, totalDomains, chunksWritten,
				float64(domainsProcessed)/float64(totalDomains)*100)

			slog.Debug("Hosts file chunk progress",
				"chunks_written", chunksWritten,
				"domains_processed", domainsProcessed,
				"total_domains", totalDomains,
				"latest_domain", domains[end-1])
		}
	}

	// Final sync
	if err := file.Sync(); err != nil {
		slog.Debug("Failed to final sync file", "error", err)
	}

	slog.Debug("Successfully wrote hosts file in chunks", "total_chunks", chunksWritten, "total_domains", totalDomains)
	log.Printf("Hosts file update completed: %d domains written in %d chunks", totalDomains, chunksWritten)

	// Set immutable flag
	slog.Debug("Setting immutable flag on hosts file", "command", "chattr +i "+hostsPath)
	if err := exec.Command("chattr", "+i", hostsPath).Run(); err != nil {
		slog.Debug("Failed to set immutable flag", "error", err)
	} else {
		slog.Debug("Successfully set immutable flag")
	}

	// Update checksum after legitimate change
	slog.Debug("Updating checksum for hosts file")
	updateChecksum(hostsPath)
	slog.Debug("Hosts file update completed successfully")

	return nil
}

func updateFirewall(domains []string, dryRun bool) error {
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

		if isIPAddress(domain) {
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
			ips := resolveIPs(domain, "A")
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
			ips6 := resolveIPs(domain, "AAAA")
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

func isIPAddress(s string) bool {
	// Try to parse as IPv4 or IPv6
	return net.ParseIP(s) != nil
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

	// Validate forbidden programs config
	if config.EnableForbiddenPrograms && config.ForbiddenPrograms.Enabled {
		for _, program := range config.ForbiddenPrograms.Programs {
			if program.Name == "" {
				return fmt.Errorf("forbidden program name cannot be empty")
			}
			for _, window := range program.TimeWindows {
				if !isValidTime(window.Start) || !isValidTime(window.End) {
					return fmt.Errorf("invalid time format for forbidden program %s (use HH:MM)", program.Name)
				}
				if len(window.Days) == 0 {
					return fmt.Errorf("time window for forbidden program %s must specify at least one day", program.Name)
				}
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
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func init() {
	// Set process priority to make it less likely to be killed by OOM
	syscall.Setpriority(syscall.PRIO_PROCESS, 0, -10)

	// Set up signal trapping
	setupSignalTrapping()
}

func setupSignalTrapping() {
	// Create a channel to receive OS signals
	sigChan := make(chan os.Signal, 1)

	// Register the channel to receive specific signals
	// We trap most termination signals but allow some system signals to pass through
	signal.Notify(sigChan,
		syscall.SIGTERM, // Termination request
		syscall.SIGINT,  // Interrupt (Ctrl+C)
		syscall.SIGQUIT, // Quit (Ctrl+\)
		syscall.SIGHUP,  // Hangup
		syscall.SIGUSR1, // User-defined signal 1
		syscall.SIGUSR2, // User-defined signal 2
		syscall.SIGPIPE, // Broken pipe
		syscall.SIGALRM, // Alarm clock
		syscall.SIGTSTP, // Terminal stop (Ctrl+Z)
		syscall.SIGCONT, // Continue after stop
	)

	// Start a goroutine to handle signals
	go handleSignals(sigChan)
}

func handleSignals(sigChan chan os.Signal) {
	for sig := range sigChan {
		// Load config to send accountability email
		config, err := loadConfig()
		if err != nil {
			log.Printf("Failed to load config for signal handling: %v", err)
			continue
		}

		// Send accountability email about the termination attempt
		if config.Accountability.Enabled {
			subject := "GLOCKER ALERT: Termination Attempt Detected"
			body := fmt.Sprintf("An attempt to terminate Glocker was detected at %s.\n\n", time.Now().Format("2006-01-02 15:04:05"))
			body += fmt.Sprintf("Signal received: %s (%d)\n", sig.String(), sig)
			body += fmt.Sprintf("Process ID: %d\n", os.Getpid())
			body += fmt.Sprintf("Parent Process ID: %d\n", os.Getppid())

			// Try to get information about who sent the signal
			if uid := os.Getuid(); uid != -1 {
				body += fmt.Sprintf("User ID: %d\n", uid)
			}
			if gid := os.Getgid(); gid != -1 {
				body += fmt.Sprintf("Group ID: %d\n", gid)
			}

			body += "\nGlocker is designed to resist termination attempts to maintain system protection.\n"
			body += "If this termination was intentional, please use the proper uninstall procedure.\n\n"
			body += "This is an automated alert from Glocker."

			// Send the email (this will handle dev mode appropriately)
			if err := sendEmail(&config, subject, body); err != nil {
				log.Printf("Failed to send signal accountability email: %v", err)
			} else {
				log.Printf("Sent accountability email for signal: %s", sig.String())
			}
		}

		// Log the signal attempt
		log.Printf("SECURITY ALERT: Received termination signal %s (%d) - ignoring", sig.String(), sig)

		// For certain signals, we might want to take additional action
		switch sig {
		case syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT:
			log.Printf("Ignoring termination signal %s - use 'glocker -uninstall' for proper removal", sig.String())
		case syscall.SIGTSTP:
			log.Printf("Ignoring stop signal %s - process cannot be suspended", sig.String())
		default:
			log.Printf("Ignoring signal %s", sig.String())
		}

		// We don't exit or stop - we just log and continue running
		// This makes the process resistant to casual termination attempts
	}
}

// ============================================================================
// TAMPER DETECTION
// ============================================================================

type FileChecksum struct {
	Path     string
	Checksum string
	Exists   bool
}

type TempUnblock struct {
	Domain    string
	ExpiresAt time.Time
}

type ContentReport struct {
	URL       string `json:"url"`
	Domain    string `json:"domain,omitempty"`
	Trigger   string `json:"trigger"`
	Timestamp int64  `json:"timestamp"`
}

type ProcessInfo struct {
	PID         string
	Name        string
	CommandLine string
	ParentPID   string
}

type Violation struct {
	Timestamp time.Time
	Host      string
	URL       string
	Type      string // "web_access", "content_report", "forbidden_program"
}

// Global variables for tamper detection
var (
	globalChecksums      []FileChecksum
	globalFilesToMonitor []string
	globalConfig         *Config
	tempUnblocks         []TempUnblock
	sseClients           []chan string
	sseClientsMutex      sync.RWMutex
	violations           []Violation
	violationsMutex      sync.RWMutex
	lastViolationReset   time.Time
)

func (f FileChecksum) String() string {
	return fmt.Sprintf("Path : %s, Checksum : %s, Exists : %v", f.Path, f.Checksum, f.Exists)
}

func monitorTampering(config *Config, checksums []FileChecksum, filesToMonitor []string) {
	// Set default check interval if not specified
	checkInterval := config.TamperDetection.CheckInterval
	if checkInterval == 0 {
		checkInterval = 30 // Default: check every 30 seconds
	}

	firewallRuleCount := countFirewallRules()

	ticker := time.NewTicker(time.Duration(checkInterval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("Tamper check")
		tampered := false
		var tamperReasons []string

		// Check file checksums
		var currentChecksums []FileChecksum
		for _, filePath := range filesToMonitor {
			checksum := captureChecksum(config, filePath)
			currentChecksums = append(currentChecksums, checksum)
		}
		for i, current := range currentChecksums {
			original := checksums[i]

			// File was deleted
			if original.Exists && !current.Exists {
				tampered = true
				tamperReasons = append(tamperReasons, fmt.Sprintf("File deleted: %s", current.Path))
			}

			// File was modified
			if original.Exists && current.Exists && original.Checksum != current.Checksum {
				tampered = true
				tamperReasons = append(tamperReasons, fmt.Sprintf("File modified: %s", current.Path))
			}
		}

		// Check firewall rules
		currentRuleCount := countFirewallRules()
		if currentRuleCount < firewallRuleCount {
			tampered = true
			reason := fmt.Sprintf("Firewall rules reduced from %d to %d", firewallRuleCount, currentRuleCount)
			tamperReasons = append(tamperReasons, reason)
		}

		// Check if service is still running
		if !isServiceRunning() {
			tampered = true
			tamperReasons = append(tamperReasons, "Glocker service was stopped")
		}

		// Trigger alarm if tampering detected
		if tampered {
			log.Println("Tamper check failed")
			log.Println(tamperReasons)
		
			// Send desktop notification
			sendNotification(config, "Glocker Security Alert", 
				"System tampering detected!",
				"critical", "dialog-error")
		
			raiseAlarm(config, tamperReasons)
			// Update baseline checksums after alarm
			checksums = nil
			for _, filePath := range filesToMonitor {
				checksum := captureChecksum(config, filePath)
				checksums = append(checksums, checksum)
			}
			// Also update global checksums
			globalChecksums = checksums
			firewallRuleCount = countFirewallRules()
		}

	}

}

func captureChecksum(config *Config, path string) FileChecksum {
	checksum := FileChecksum{Path: path}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		checksum.Exists = false
	} else {
		checksum.Exists = true

		// For hosts file, only checksum the GLOCKER section
		if path == config.HostsPath {
			if data, err := os.ReadFile(path); err == nil {
				glockerSection := extractGlockerSection(string(data))
				hash := sha256.Sum256([]byte(glockerSection))
				checksum.Checksum = fmt.Sprintf("%x", hash)
			}
		} else {
			// Calculate SHA256 checksum for other files
			if data, err := os.ReadFile(path); err == nil {
				hash := sha256.Sum256(data)
				checksum.Checksum = fmt.Sprintf("%x", hash)
			}
		}
	}

	return checksum
}

func countFirewallRules() int {
	count := 0

	// Count IPv4 rules
	cmd := exec.Command("bash", "-c", "iptables -S OUTPUT | grep 'GLOCKER-BLOCK' | wc -l")
	if output, err := cmd.Output(); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &count)
	}

	// Count IPv6 rules
	cmd = exec.Command("bash", "-c", "ip6tables -S OUTPUT | grep 'GLOCKER-BLOCK' | wc -l")
	if output, err := cmd.Output(); err == nil {
		var count6 int
		fmt.Sscanf(strings.TrimSpace(string(output)), "%d", &count6)
		count += count6
	}

	return count
}

func isServiceRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "glocker.service")
	output, _ := cmd.Output()
	return strings.TrimSpace(string(output)) == "active"
}

func raiseAlarm(config *Config, reasons []string) {
	if config.TamperDetection.AlarmCommand == "" {
		return
	}

	// Prepare alarm message
	message := "GLOCKER TAMPER DETECTED:\\n"
	for _, reason := range reasons {
		message += "  - " + reason + "\\n"
	}

	// Send accountability email
	if config.Accountability.Enabled {
		subject := "GLOCKER ALERT: Tampering Detected"
		body := fmt.Sprintf("Tampering was detected at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
		for _, reason := range reasons {
			body += "  - " + reason + "\n"
		}
		body += "\nThis is an automated alert from Glocker."

		sendEmail(config, subject, body)
	}

	// Execute alarm command - split on spaces for proper argument handling
	parts := strings.Fields(config.TamperDetection.AlarmCommand)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(),
		"GLOCKER_TAMPER_MESSAGE="+message,
		"GLOCKER_TAMPER_REASONS="+strings.Join(reasons, "; "),
	)

	cmd.Run()
}

func extractGlockerSection(content string) string {
	lines := strings.Split(content, "\n")
	var glockerLines []string
	inGlockerSection := false

	for _, line := range lines {
		if strings.Contains(line, HOSTS_MARKER_START) {
			inGlockerSection = true
			glockerLines = append(glockerLines, line)
			continue
		}
		if inGlockerSection {
			glockerLines = append(glockerLines, line)
		}
	}

	return strings.Join(glockerLines, "\n")
}

// updateChecksum updates the checksum for a specific file in the global checksums
func updateChecksum(filePath string) {
	if globalConfig == nil || len(globalChecksums) == 0 {
		return // Tamper detection not initialized
	}

	// Find the index of the file in our monitoring list
	fileIndex := -1
	for i, monitoredFile := range globalFilesToMonitor {
		if monitoredFile == filePath {
			fileIndex = i
			break
		}
	}

	if fileIndex == -1 {
		return // File not being monitored
	}

	// Update the checksum
	newChecksum := captureChecksum(globalConfig, filePath)
	globalChecksums[fileIndex] = newChecksum
	log.Printf("Updated checksum for %s: %s", filePath, newChecksum.Checksum)
}

func unblockHostsFromFlag(config *Config, hostsStr string) {
	// Parse the format: "domain1,domain2:reason"
	parts := strings.SplitN(hostsStr, ":", 2)
	if len(parts) != 2 {
		log.Fatal("ERROR: Reason required. Use format: 'domain1,domain2:reason'")
	}

	domains := strings.TrimSpace(parts[0])
	reason := strings.TrimSpace(parts[1])

	if domains == "" {
		log.Fatal("ERROR: No domains specified")
	}

	if reason == "" {
		log.Fatal("ERROR: Reason cannot be empty")
	}

	payload := fmt.Sprintf("%s:%s", domains, reason)
	sendSocketMessage("unblock", payload)
	log.Printf("Domains will be temporarily unblocked (Reason: %s) and automatically re-blocked after the configured time.", reason)
}

func blockHostsFromFlag(config *Config, hostsStr string) {
	sendSocketMessage("block", hostsStr)
	log.Println("Domains will be permanently blocked.")
}

func addKeywordsFromFlag(config *Config, keywordsStr string) {
	sendSocketMessage("add-keyword", keywordsStr)
	log.Println("Keywords will be added to both URL and content keyword lists.")
}

func sendSocketMessage(action, domains string) {
	conn, err := net.Dial("unix", "/tmp/glocker.sock")
	if err != nil {
		log.Fatalf("Failed to connect to glocker service: %v", err)
	}
	defer conn.Close()

	var message string
	if domains != "" {
		message = fmt.Sprintf("%s:%s\n", action, domains)
	} else {
		message = fmt.Sprintf("%s\n", action)
	}

	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	// Read response
	response, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	log.Printf("Response: %s", strings.TrimSpace(response))
}

func handleReloadRequest() {
	conn, err := net.Dial("unix", "/tmp/glocker.sock")
	if err != nil {
		log.Fatalf("Failed to connect to glocker service: %v", err)
	}
	defer conn.Close()

	message := "reload\n"

	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Fatalf("Failed to send reload message: %v", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	log.Printf("Response: %s", strings.TrimSpace(response))
}

func handleUninstallRequest(reason string) {
	conn, err := net.Dial("unix", "/tmp/glocker.sock")
	if err != nil {
		log.Fatalf("Failed to connect to glocker service: %v", err)
	}
	defer conn.Close()

	message := fmt.Sprintf("uninstall:%s\n", reason)

	_, err = conn.Write([]byte(message))
	if err != nil {
		log.Fatalf("Failed to send uninstall message: %v", err)
	}

	// Read initial response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read initial response: %v", err)
	}

	log.Printf("Response: %s", strings.TrimSpace(response))

	// Wait for completion signal
	log.Println("Waiting for uninstall process to complete...")
	completionResponse, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read completion response: %v", err)
	}

	log.Printf("Completion: %s", strings.TrimSpace(completionResponse))

	// Now stop and disable the systemd service
	log.Println("Stopping and disabling glocker service...")
	if err := exec.Command("systemctl", "stop", "glocker.service").Run(); err != nil {
		log.Printf("   Warning: couldn't stop service: %v", err)
	} else {
		log.Println("✓ Service stopped")
	}

	if err := exec.Command("systemctl", "disable", "glocker.service").Run(); err != nil {
		log.Printf("   Warning: couldn't disable service: %v", err)
	} else {
		log.Println("✓ Service disabled")
	}

	// Reload systemd daemon
	log.Println("Reloading systemd daemon...")
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		log.Printf("   Warning: couldn't reload systemd daemon: %v", err)
	} else {
		log.Println("✓ Systemd daemon reloaded")
	}

	// Now print the manual deletion commands
	log.Println()
	log.Println("🎉 Glocker system changes have been restored!")
	log.Println("   All protections have been removed and original settings restored.")
	log.Printf("   Uninstall reason: %s", reason)
	log.Println()
	log.Println("To complete the uninstall, manually run these commands:")
	log.Printf("   rm -f %s", "/etc/systemd/system/glocker.service")
	log.Printf("   rm -f %s", INSTALL_PATH)
	log.Printf("   rm -f %s", GLOCKER_CONFIG_FILE)
	log.Printf("   rmdir %s", filepath.Dir(GLOCKER_CONFIG_FILE))
}

func handleStatusCommand(config *Config) {
	// Check if socket exists and is accessible
	if _, err := os.Stat(GLOCKER_SOCK); err == nil {
		// Socket exists, try to connect and get live status
		conn, err := net.Dial("unix", GLOCKER_SOCK)
		if err == nil {
			defer conn.Close()

			log.Println("=== LIVE STATUS ===")

			// Send status request
			conn.Write([]byte("status\n"))

			// Read response
			scanner := bufio.NewScanner(conn)
			for scanner.Scan() {
				line := scanner.Text()
				if line == "END" {
					break
				}
				fmt.Println(line)
			}
			return
		}
	}

	// Socket not available, fall back to static status
	log.Println("=== STATIC STATUS ===")
	log.Println("(Service not running - showing configuration only)")
	printConfig(config)
	runOnce(config, true)
}

func getStatusResponse(config *Config) string {
	var response strings.Builder
	now := time.Now()

	response.WriteString("╔════════════════════════════════════════════════╗\n")
	response.WriteString("║                 LIVE STATUS                    ║\n")
	response.WriteString("╚════════════════════════════════════════════════╝\n")
	response.WriteString("\n")

	response.WriteString(fmt.Sprintf("Current Time: %s\n", now.Format("2006-01-02 15:04:05")))
	response.WriteString(fmt.Sprintf("Service Status: Running\n"))
	response.WriteString(fmt.Sprintf("Enforcement Interval: %d seconds\n", config.EnforceInterval))
	response.WriteString("\n")

	// Get currently blocked domains
	blockedDomains := getDomainsToBlock(config, now)
	response.WriteString(fmt.Sprintf("Currently Blocked Domains: %d\n", len(blockedDomains)))

	// Show temporary unblocks
	activeUnblocks := 0
	for _, unblock := range tempUnblocks {
		if now.Before(unblock.ExpiresAt) {
			activeUnblocks++
		}
	}
	response.WriteString(fmt.Sprintf("Temporary Unblocks: %d active\n", activeUnblocks))

	if activeUnblocks > 0 {
		response.WriteString("  Active temporary unblocks:\n")
		for _, unblock := range tempUnblocks {
			if now.Before(unblock.ExpiresAt) {
				remaining := unblock.ExpiresAt.Sub(now)
				response.WriteString(fmt.Sprintf("    - %s (expires in %v)\n", unblock.Domain, remaining.Round(time.Minute)))
			}
		}
	}
	response.WriteString("\n")

	// Configuration summary
	response.WriteString("Configuration:\n")
	response.WriteString(fmt.Sprintf("  Hosts File Management: %v\n", config.EnableHosts))
	response.WriteString(fmt.Sprintf("  Firewall Management: %v\n", config.EnableFirewall))
	response.WriteString(fmt.Sprintf("  Forbidden Programs: %v\n", config.EnableForbiddenPrograms && config.ForbiddenPrograms.Enabled))
	response.WriteString(fmt.Sprintf("  Sudoers Management: %v\n", config.Sudoers.Enabled))
	response.WriteString(fmt.Sprintf("  Tamper Detection: %v\n", config.TamperDetection.Enabled))
	response.WriteString(fmt.Sprintf("  Accountability: %v\n", config.Accountability.Enabled))
	response.WriteString(fmt.Sprintf("  Web Tracking: %v\n", config.WebTracking.Enabled))
	response.WriteString(fmt.Sprintf("  Content Monitoring: %v\n", config.ContentMonitoring.Enabled))
	response.WriteString("\n")

	// Count domains by type
	alwaysBlockCount := 0
	timeBasedCount := 0
	loggedCount := 0
	for _, domain := range config.Domains {
		if domain.AlwaysBlock {
			alwaysBlockCount++
		} else {
			timeBasedCount++
		}
		if domain.LogBlocking {
			loggedCount++
		}
	}

	response.WriteString(fmt.Sprintf("Total Domains: %d (%d always blocked, %d time-based, %d with detailed logging)\n",
		len(config.Domains), alwaysBlockCount, timeBasedCount, loggedCount))

	// Show violation tracking status
	if config.ViolationTracking.Enabled {
		violationsMutex.RLock()
		recentViolations := countRecentViolations(config, now)
		totalViolations := len(violations)
		violationsMutex.RUnlock()

		response.WriteString("\n")
		response.WriteString("Violation Tracking:\n")
		response.WriteString(fmt.Sprintf("  Recent Violations: %d/%d (in last %d minutes)\n",
			recentViolations, config.ViolationTracking.MaxViolations, config.ViolationTracking.TimeWindowMinutes))
		response.WriteString(fmt.Sprintf("  Total Violations: %d\n", totalViolations))
		if config.ViolationTracking.ResetDaily {
			response.WriteString(fmt.Sprintf("  Last Reset: %s\n", lastViolationReset.Format("2006-01-02 15:04:05")))
		}
	}

	response.WriteString("END\n")
	return response.String()
}

func printConfig(config *Config) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║                 CONFIGURATION                  ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()

	fmt.Printf("Hosts File Management: %v\n", config.EnableHosts)
	if config.EnableHosts {
		fmt.Printf("  Hosts Path: %s\n", config.HostsPath)
	}

	fmt.Printf("Firewall Management: %v\n", config.EnableFirewall)
	fmt.Printf("Forbidden Programs: %v\n", config.EnableForbiddenPrograms && config.ForbiddenPrograms.Enabled)
	if config.EnableForbiddenPrograms && config.ForbiddenPrograms.Enabled {
		fmt.Printf("  Check Interval: %d seconds\n", config.ForbiddenPrograms.CheckInterval)
		fmt.Printf("  Programs: %d configured\n", len(config.ForbiddenPrograms.Programs))
	}
	fmt.Printf("Self Healing: %v\n", config.SelfHeal)
	fmt.Printf("Enforcement Interval: %d seconds\n", config.EnforceInterval)
	fmt.Printf("Mindful Delay: %d seconds\n", config.MindfulDelay)
	fmt.Printf("Temp Unblock Time: %d minutes\n", config.TempUnblockTime)

	fmt.Println()

	// Count domains by type
	alwaysBlockCount := 0
	timeBasedCount := 0
	for _, domain := range config.Domains {
		if domain.AlwaysBlock {
			alwaysBlockCount++
		} else {
			timeBasedCount++
		}
	}

	// Count domains with logging enabled
	loggedCount := 0
	for _, domain := range config.Domains {
		if domain.LogBlocking {
			loggedCount++
		}
	}

	fmt.Printf("Domains: %d total (%d always blocked, %d time-based, %d with detailed logging)\n",
		len(config.Domains), alwaysBlockCount, timeBasedCount, loggedCount)

	fmt.Println()
	fmt.Printf("Sudoers Management: %v\n", config.Sudoers.Enabled)
	if config.Sudoers.Enabled {
		fmt.Printf("  User: %s\n", config.Sudoers.User)
		fmt.Printf("  Allowed Line: %s\n", config.Sudoers.AllowedSudoersLine)
		fmt.Printf("  Blocked Line: %s\n", config.Sudoers.BlockedSudoersLine)
		fmt.Printf("  Time Windows: %d configured\n", len(config.Sudoers.TimeAllowed))
	}

	fmt.Println()
	fmt.Printf("Tamper Detection: %v\n", config.TamperDetection.Enabled)
	if config.TamperDetection.Enabled {
		fmt.Printf("  Check Interval: %d seconds\n", config.TamperDetection.CheckInterval)
		fmt.Printf("  Alarm Command: %s\n", config.TamperDetection.AlarmCommand)
	}

	fmt.Println()
	fmt.Printf("Accountability: %v\n", config.Accountability.Enabled)
	if config.Accountability.Enabled {
		fmt.Printf("  Partner Email: %s\n", config.Accountability.PartnerEmail)
		fmt.Printf("  From Email: %s\n", config.Accountability.FromEmail)
		fmt.Printf("  Daily Report: %v\n", config.Accountability.DailyReportEnabled)
		if config.Accountability.DailyReportEnabled {
			fmt.Printf("  Daily Report Time: %s\n", config.Accountability.DailyReportTime)
		}
	}

	fmt.Println()
	fmt.Printf("Web Tracking: %v\n", config.WebTracking.Enabled)
	if config.WebTracking.Enabled {
		fmt.Printf("  Command: %s\n", config.WebTracking.Command)
	}

	fmt.Println()
	fmt.Printf("Content Monitoring: %v\n", config.ContentMonitoring.Enabled)
	if config.ContentMonitoring.Enabled {
		fmt.Printf("  Log File: %s\n", config.ContentMonitoring.LogFile)
	}

	fmt.Println()
	fmt.Printf("Violation Tracking: %v\n", config.ViolationTracking.Enabled)
	if config.ViolationTracking.Enabled {
		fmt.Printf("  Max Violations: %d\n", config.ViolationTracking.MaxViolations)
		fmt.Printf("  Time Window: %d minutes\n", config.ViolationTracking.TimeWindowMinutes)
		fmt.Printf("  Command: %s\n", config.ViolationTracking.Command)
		fmt.Printf("  Reset Daily: %v\n", config.ViolationTracking.ResetDaily)
		if config.ViolationTracking.ResetDaily {
			fmt.Printf("  Reset Time: %s\n", config.ViolationTracking.ResetTime)
		}
	}

	fmt.Println()
}

func startWebTrackingServer(config *Config) {
	slog.Debug("Starting web tracking servers on ports 80 and 443")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleWebTrackingRequest(config, w, r)
	})

	// Add report endpoint for browser extensions
	http.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		handleReportRequest(config, w, r)
	})

	// Add keywords endpoint for browser extensions
	http.HandleFunc("/keywords", func(w http.ResponseWriter, r *http.Request) {
		handleKeywordsRequest(config, w, r)
	})

	// Add SSE endpoint for real-time keyword updates
	http.HandleFunc("/keywords-stream", func(w http.ResponseWriter, r *http.Request) {
		handleSSERequest(config, w, r)
	})

	// Add blocked page endpoint
	http.HandleFunc("/blocked", func(w http.ResponseWriter, r *http.Request) {
		handleBlockedPageRequest(w, r)
	})

	// Start HTTP server
	go func() {
		server := &http.Server{
			Addr:    ":80",
			Handler: nil,
		}

		log.Printf("Web tracking HTTP server started on port 80")
		if err := server.ListenAndServe(); err != nil {
			log.Printf("Web tracking HTTP server error: %v", err)
		}
	}()

	// Start HTTPS server
	go func() {
		server := &http.Server{
			Addr:    ":443",
			Handler: nil,
		}

		// Generate self-signed certificate
		certFile, keyFile, err := generateSelfSignedCert()
		if err != nil {
			log.Printf("Failed to generate SSL certificate: %v", err)
			return
		}
		defer os.Remove(certFile)
		defer os.Remove(keyFile)

		log.Printf("Web tracking HTTPS server started on port 443")
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
			log.Printf("Web tracking HTTPS server error: %v", err)
		}
	}()
}

func handleWebTrackingRequest(config *Config, w http.ResponseWriter, r *http.Request) {
	// Check for blocked page first, before any other processing
	if r.URL.Path == "/blocked" {
		handleBlockedPageRequest(w, r)
		return
	}

	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}

	// Remove port if present
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	slog.Debug("Web tracking request received", "host", host, "url", r.URL.String(), "method", r.Method)

	// Check if this host is in our blocked domains list
	isBlocked := false
	matchedDomain := ""
	now := time.Now()
	blockedDomains := getDomainsToBlock(config, now)

	for _, blockedDomain := range blockedDomains {
		// Direct match
		if host == blockedDomain {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
		// www subdomain match
		if host == "www."+blockedDomain {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
		// Any subdomain match
		if strings.HasSuffix(host, "."+blockedDomain) {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
		// Reverse check - if host is the base domain and blocked domain has www
		if strings.HasPrefix(blockedDomain, "www.") && host == blockedDomain[4:] {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
	}

	slog.Debug("Host blocking check", "host", host, "is_blocked", isBlocked, "matched_domain", matchedDomain, "blocked_domains_count", len(blockedDomains))

	if isBlocked {
		// Determine the blocking reason
		blockingReason := getBlockingReason(config, matchedDomain, time.Now())
		log.Printf("BLOCKED SITE ACCESS: %s -> matched domain: %s -> reason: %s", host, matchedDomain, blockingReason)

		// Record violation
		if config.ViolationTracking.Enabled {
			recordViolation(config, "web_access", host, r.URL.String())
		}

		// Execute the configured command
		if config.WebTracking.Command != "" {
			go executeWebTrackingCommand(config, host, r)
		}

		// Send desktop notification
		sendNotification(config, "Glocker Alert", 
			fmt.Sprintf("Blocked access to %s", host),
			"normal", "dialog-information")

		// Send accountability email
		if config.Accountability.Enabled {
			subject := "GLOCKER ALERT: Blocked Site Access Attempt"
			body := fmt.Sprintf("An attempt to access a blocked site was detected at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
			body += fmt.Sprintf("Host: %s\n", host)
			body += fmt.Sprintf("Matched Domain: %s\n", matchedDomain)
			body += fmt.Sprintf("Blocking Reason: %s\n", blockingReason)
			body += fmt.Sprintf("URL: %s\n", r.URL.String())
			body += fmt.Sprintf("Method: %s\n", r.Method)
			body += fmt.Sprintf("User-Agent: %s\n", r.Header.Get("User-Agent"))
			body += fmt.Sprintf("Remote Address: %s\n", r.RemoteAddr)
			body += "\nThis is an automated alert from Glocker."

			if err := sendEmail(config, subject, body); err != nil {
				log.Printf("Failed to send web tracking accountability email: %v", err)
			}
		}

		// Redirect to localhost blocked page to avoid double violation
		blockedURL := fmt.Sprintf("http://127.0.0.1/blocked?domain=%s&matched=%s&url=%s", host, matchedDomain, r.URL.String())
		http.Redirect(w, r, blockedURL, http.StatusFound)
	} else {
		// Not a blocked domain, return a simple response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

func executeWebTrackingCommand(config *Config, host string, r *http.Request) {
	if config.WebTracking.Command == "" {
		return
	}

	slog.Debug("Executing web tracking command", "host", host, "command", config.WebTracking.Command)

	// Split command into parts for proper execution
	parts := strings.Fields(config.WebTracking.Command)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	// Set environment variables with information about the blocked access attempt
	cmd.Env = append(os.Environ(),
		"GLOCKER_BLOCKED_HOST="+host,
		"GLOCKER_BLOCKED_URL="+r.URL.String(),
		"GLOCKER_BLOCKED_METHOD="+r.Method,
		"GLOCKER_BLOCKED_USER_AGENT="+r.Header.Get("User-Agent"),
		"GLOCKER_BLOCKED_REMOTE_ADDR="+r.RemoteAddr,
		"GLOCKER_BLOCKED_TIME="+time.Now().Format("2006-01-02 15:04:05"),
	)

	if err := cmd.Run(); err != nil {
		log.Printf("Failed to execute web tracking command: %v", err)
	} else {
		slog.Debug("Web tracking command executed successfully", "host", host)
	}
}

func handleKeywordsRequest(config *Config, w http.ResponseWriter, r *http.Request) {
	// Only accept GET requests
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Set CORS headers to allow browser extension access
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	// Combine content keywords with URL keywords
	combinedContentKeywords := make([]string, 0, len(config.ExtensionKeywords.ContentKeywords)+len(config.ExtensionKeywords.URLKeywords))
	combinedContentKeywords = append(combinedContentKeywords, config.ExtensionKeywords.ContentKeywords...)
	combinedContentKeywords = append(combinedContentKeywords, config.ExtensionKeywords.URLKeywords...)

	// Create response with keywords
	response := map[string]interface{}{
		"url_keywords":     config.ExtensionKeywords.URLKeywords,
		"content_keywords": combinedContentKeywords,
		"whitelist":        config.ExtensionKeywords.Whitelist,
	}

	// Encode and send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Debug("Failed to encode keywords response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	slog.Debug("Keywords request served", "url_keywords_count", len(config.ExtensionKeywords.URLKeywords), "content_keywords_count", len(combinedContentKeywords))
}

func handleReportRequest(config *Config, w http.ResponseWriter, r *http.Request) {
	// Only accept POST requests

	slog.Info("Got a request here", "method", r.Method, "value", http.MethodPost)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check if content monitoring is enabled
	if !config.ContentMonitoring.Enabled {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Parse JSON body
	var report ContentReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		slog.Debug("Failed to parse report JSON", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Record violation
	if config.ViolationTracking.Enabled {
		recordViolation(config, "content_report", report.Domain, report.URL)
	}

	// Log the report
	if err := logContentReport(config, &report); err != nil {
		slog.Debug("Failed to log content report", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	slog.Debug("Content report logged", "url", report.URL, "trigger", report.Trigger)
	log.Printf("CONTENT REPORT: %s - %s", report.Trigger, report.URL)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func logContentReport(config *Config, report *ContentReport) error {
	logFile := config.ContentMonitoring.LogFile
	if logFile == "" {
		logFile = "/var/log/glocker-reports.log"
	}

	// Create log entry
	timestamp := time.Unix(report.Timestamp/1000, 0).Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] | %s | %s", timestamp, report.Trigger, report.URL)
	if report.Domain != "" {
		logEntry += fmt.Sprintf(" | %s", report.Domain)
	}
	logEntry += "\n"

	// Append to log file
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(logEntry)
	if err != nil {
		return fmt.Errorf("failed to write to log file: %w", err)
	}

	return nil
}

func isValidUnblockReason(config *Config, reason string) bool {
	// If no reasons are configured, allow any reason
	if len(config.Unblocking.Reasons) == 0 {
		return true
	}

	// Check if the provided reason matches any of the configured valid reasons
	for _, validReason := range config.Unblocking.Reasons {
		if strings.EqualFold(reason, validReason) {
			return true
		}
	}

	return false
}

func generateSelfSignedCert() (string, string, error) {
	// Generate a private key
	priv, err := exec.Command("openssl", "genrsa", "-out", "/tmp/glocker-key.pem", "2048").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %v, output: %s", err, priv)
	}

	// Generate a self-signed certificate
	cert, err := exec.Command("openssl", "req", "-new", "-x509", "-key", "/tmp/glocker-key.pem",
		"-out", "/tmp/glocker-cert.pem", "-days", "365", "-subj", "/CN=localhost").CombinedOutput()
	if err != nil {
		os.Remove("/tmp/glocker-key.pem")
		return "", "", fmt.Errorf("failed to generate certificate: %v, output: %s", err, cert)
	}

	return "/tmp/glocker-cert.pem", "/tmp/glocker-key.pem", nil
}

func monitorForbiddenPrograms(config *Config) {
	// Set default check interval if not specified
	checkInterval := config.ForbiddenPrograms.CheckInterval
	if checkInterval == 0 {
		checkInterval = 5 // Default: check every 5 seconds
	}

	slog.Debug("Starting forbidden programs monitoring", "check_interval_seconds", checkInterval, "programs_count", len(config.ForbiddenPrograms.Programs))

	ticker := time.NewTicker(time.Duration(checkInterval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		currentDay := now.Weekday().String()[:3]
		currentTime := now.Format("15:04")

		slog.Debug("Checking for forbidden programs", "current_day", currentDay, "current_time", currentTime)

		for _, program := range config.ForbiddenPrograms.Programs {
			// Check if any time window is active for this program
			programForbidden := false
			for _, window := range program.TimeWindows {
				if !slices.Contains(window.Days, currentDay) {
					continue
				}

				if isInTimeWindow(currentTime, window.Start, window.End) {
					programForbidden = true
					slog.Debug("Program is forbidden in current time window", "program", program.Name, "window", fmt.Sprintf("%s-%s", window.Start, window.End))
					break
				}
			}

			if programForbidden {
				killMatchingProcesses(config, program.Name)
			}
		}
	}
}

func killMatchingProcesses(config *Config, programName string) {
	// Get list of running processes
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Failed to get process list", "error", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	killedProcesses := []string{}
	processGroups := make(map[string][]ProcessInfo) // Group by parent PID

	// First pass: collect all matching processes and group by parent
	for _, line := range lines {
		// Skip header line and empty lines
		if strings.Contains(line, "USER") || strings.TrimSpace(line) == "" {
			continue
		}

		// Check if the process name contains our forbidden program name (case-insensitive)
		if strings.Contains(strings.ToLower(line), strings.ToLower(programName)) {
			// Extract PID (second column in ps aux output)
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}

			pid := fields[1]
			processName := extractProcessName(line)

			// Don't kill our own process or system processes
			if strings.Contains(strings.ToLower(processName), "glocker") ||
				strings.Contains(strings.ToLower(processName), "systemd") ||
				strings.Contains(strings.ToLower(processName), "kernel") ||
				pid == "1" {
				continue
			}

			// Get parent PID to group processes
			parentPID := getParentPID(pid)
			
			processInfo := ProcessInfo{
				PID:         pid,
				Name:        processName,
				CommandLine: line,
				ParentPID:   parentPID,
			}

			// Group by parent PID (or by own PID if it's a parent process)
			groupKey := parentPID
			if parentPID == "1" || parentPID == "" {
				groupKey = pid // This is likely a parent process itself
			}

			processGroups[groupKey] = append(processGroups[groupKey], processInfo)
		}
	}

	// Second pass: kill parent processes (which should kill their children too)
	for groupKey, processes := range processGroups {
		if len(processes) == 0 {
			continue
		}

		// Find the parent process in this group (the one whose PID matches the group key)
		var parentProcess *ProcessInfo
		for i := range processes {
			if processes[i].PID == groupKey {
				parentProcess = &processes[i]
				break
			}
		}

		// If we didn't find the parent in our matching processes, use the first one
		if parentProcess == nil {
			parentProcess = &processes[0]
		}

		slog.Debug("Found forbidden process group", "program_filter", programName, "parent_pid", parentProcess.PID, "parent_name", parentProcess.Name, "child_count", len(processes)-1)

		// Record only one violation per process group
		if config.ViolationTracking.Enabled {
			recordViolation(config, "forbidden_program", parentProcess.Name, fmt.Sprintf("PID: %s (with %d children)", parentProcess.PID, len(processes)-1))
		}

		// Kill the parent process (this should terminate children too)
		if err := exec.Command("kill", parentProcess.PID).Run(); err == nil {
			killedProcesses = append(killedProcesses, fmt.Sprintf("%s (PID: %s, %d children)", parentProcess.Name, parentProcess.PID, len(processes)-1))
			log.Printf("KILLED FORBIDDEN PROGRAM GROUP: %s (PID: %s) with %d children - matched filter: %s", parentProcess.Name, parentProcess.PID, len(processes)-1, programName)

			// Send desktop notification
			sendNotification(config, "Glocker Alert", 
				fmt.Sprintf("Terminated forbidden program: %s", parentProcess.Name),
				"normal", "dialog-warning")

			// Wait a moment then force kill if still running
			time.Sleep(2 * time.Second)
			exec.Command("kill", "-9", parentProcess.PID).Run()

			// Also force kill any remaining children
			for _, process := range processes {
				if process.PID != parentProcess.PID {
					exec.Command("kill", "-9", process.PID).Run()
				}
			}
		} else {
			slog.Debug("Failed to kill parent process", "pid", parentProcess.PID, "error", err)
			
			// If parent kill failed, try to kill all processes in the group individually
			for _, process := range processes {
				if err := exec.Command("kill", process.PID).Run(); err == nil {
					killedProcesses = append(killedProcesses, fmt.Sprintf("%s (PID: %s)", process.Name, process.PID))
					time.Sleep(1 * time.Second)
					exec.Command("kill", "-9", process.PID).Run()
				}
			}
		}
	}

	// Send accountability email if processes were killed
	if len(killedProcesses) > 0 && config.Accountability.Enabled {
		subject := "GLOCKER ALERT: Forbidden Programs Terminated"
		body := fmt.Sprintf("Forbidden programs were detected and terminated at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
		body += fmt.Sprintf("Filter: %s\n", programName)
		body += "Terminated process groups:\n"
		for _, process := range killedProcesses {
			body += fmt.Sprintf("  - %s\n", process)
		}
		body += "\nThese programs are configured to be blocked during specified time windows.\n"
		body += "\nThis is an automated alert from Glocker."

		if err := sendEmail(config, subject, body); err != nil {
			log.Printf("Failed to send forbidden programs accountability email: %v", err)
		}
	}
}

func getParentPID(pid string) string {
	cmd := exec.Command("ps", "-o", "ppid=", "-p", pid)
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Failed to get parent PID", "pid", pid, "error", err)
		return "1" // Default to init if we can't determine
	}
	return strings.TrimSpace(string(output))
}

func extractProcessName(psLine string) string {
	// ps aux format: USER PID %CPU %MEM VSZ RSS TTY STAT START TIME COMMAND
	fields := strings.Fields(psLine)
	if len(fields) >= 11 {
		// Command is everything from field 10 onwards
		command := strings.Join(fields[10:], " ")
		// Extract just the program name (first part before any arguments)
		if spaceIndex := strings.Index(command, " "); spaceIndex != -1 {
			command = command[:spaceIndex]
		}
		// Remove path if present
		if slashIndex := strings.LastIndex(command, "/"); slashIndex != -1 {
			command = command[slashIndex+1:]
		}
		return command
	}
	return "unknown"
}

func handleSSERequest(config *Config, w http.ResponseWriter, r *http.Request) {
	// Only accept GET requests
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel for this client
	clientChan := make(chan string, 10)

	// Add client to the list
	sseClientsMutex.Lock()
	sseClients = append(sseClients, clientChan)
	clientIndex := len(sseClients) - 1
	sseClientsMutex.Unlock()

	slog.Debug("SSE client connected", "client_index", clientIndex, "total_clients", len(sseClients))

	// Combine content keywords with URL keywords for initial send
	combinedContentKeywords := make([]string, 0, len(config.ExtensionKeywords.ContentKeywords)+len(config.ExtensionKeywords.URLKeywords))
	combinedContentKeywords = append(combinedContentKeywords, config.ExtensionKeywords.ContentKeywords...)
	combinedContentKeywords = append(combinedContentKeywords, config.ExtensionKeywords.URLKeywords...)

	// Send initial keywords
	initialKeywords := map[string]interface{}{
		"url_keywords":     config.ExtensionKeywords.URLKeywords,
		"content_keywords": combinedContentKeywords,
		"whitelist":        config.ExtensionKeywords.Whitelist,
	}
	if keywordsJSON, err := json.Marshal(initialKeywords); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", keywordsJSON)
		w.(http.Flusher).Flush()
	}

	// Handle client disconnect
	defer func() {
		sseClientsMutex.Lock()
		// Remove client from slice
		if clientIndex < len(sseClients) {
			sseClients = append(sseClients[:clientIndex], sseClients[clientIndex+1:]...)
		}
		sseClientsMutex.Unlock()
		close(clientChan)
		slog.Debug("SSE client disconnected", "client_index", clientIndex, "remaining_clients", len(sseClients))
	}()

	// Keep connection alive and send updates
	for {
		select {
		case message := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", message)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			// Send keepalive ping
			fmt.Fprintf(w, ": keepalive\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func processAddKeywordRequest(config *Config, keywordsStr string) {
	slog.Debug("Processing add keyword request", "keywords_string", keywordsStr)

	keywords := strings.Split(keywordsStr, ",")
	var validKeywords []string

	// Clean and validate keywords
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword != "" {
			validKeywords = append(validKeywords, keyword)
			slog.Debug("Added valid keyword", "keyword", keyword)
		}
	}

	if len(validKeywords) == 0 {
		slog.Debug("No valid keywords provided")
		return
	}

	slog.Debug("Valid keywords for adding", "count", len(validKeywords), "keywords", validKeywords)
	log.Printf("Adding %d keywords to both URL and content keyword lists: %v", len(validKeywords), validKeywords)

	// Add keywords to both URL and content keyword lists
	for _, keyword := range validKeywords {
		// Check if keyword already exists in URL keywords
		if !slices.Contains(config.ExtensionKeywords.URLKeywords, keyword) {
			config.ExtensionKeywords.URLKeywords = append(config.ExtensionKeywords.URLKeywords, keyword)
			log.Printf("Added keyword %s to URL keywords", keyword)
		} else {
			log.Printf("Keyword %s already exists in URL keywords", keyword)
		}

		// Check if keyword already exists in content keywords
		if !slices.Contains(config.ExtensionKeywords.ContentKeywords, keyword) {
			config.ExtensionKeywords.ContentKeywords = append(config.ExtensionKeywords.ContentKeywords, keyword)
			log.Printf("Added keyword %s to content keywords", keyword)
		} else {
			log.Printf("Keyword %s already exists in content keywords", keyword)
		}
	}

	// Broadcast keyword update to connected browser extensions
	broadcastKeywordUpdate(config)

	log.Println("Keywords have been added successfully!")
}

func processReloadRequest(config *Config, conn net.Conn) {
	log.Println("Processing configuration reload request...")

	// Load new configuration from file
	newConfig, err := loadConfig()
	if err != nil {
		log.Printf("Failed to reload config: %v", err)
		conn.Write([]byte("ERROR: Failed to reload configuration\n"))
		return
	}

	// Validate the new configuration
	if err := validateConfig(&newConfig); err != nil {
		log.Printf("Invalid new config: %v", err)
		conn.Write([]byte("ERROR: Invalid configuration\n"))
		return
	}

	// Update global config reference
	*config = newConfig
	if globalConfig != nil {
		*globalConfig = newConfig
	}

	// Setup logging with new config
	setupLogging(config)

	log.Println("Configuration reloaded successfully")
	log.Printf("New config - Domains: %d, Hosts: %v, Firewall: %v, Sudoers: %v",
		len(config.Domains), config.EnableHosts, config.EnableFirewall, config.Sudoers.Enabled)

	// Broadcast keyword update to connected browser extensions if keywords changed
	broadcastKeywordUpdate(config)

	// Apply the new configuration immediately
	runOnce(config, false)

	// Update checksums for tamper detection after legitimate config change
	if config.TamperDetection.Enabled && globalConfig != nil {
		updateChecksum(GLOCKER_CONFIG_FILE)
		log.Println("Updated checksum for config file after reload")
	}

	// Send accountability email about config reload
	if config.Accountability.Enabled {
		subject := "GLOCKER ALERT: Configuration Reloaded"
		body := fmt.Sprintf("Glocker configuration was reloaded at %s.\n\n", time.Now().Format("2006-01-02 15:04:05"))
		body += fmt.Sprintf("New configuration summary:\n")
		body += fmt.Sprintf("  - Domains: %d\n", len(config.Domains))
		body += fmt.Sprintf("  - Hosts Management: %v\n", config.EnableHosts)
		body += fmt.Sprintf("  - Firewall Management: %v\n", config.EnableFirewall)
		body += fmt.Sprintf("  - Sudoers Management: %v\n", config.Sudoers.Enabled)
		body += fmt.Sprintf("  - Tamper Detection: %v\n", config.TamperDetection.Enabled)
		body += fmt.Sprintf("  - Accountability: %v\n", config.Accountability.Enabled)
		body += "\nAll protections have been updated with the new configuration.\n"
		body += "\nThis is an automated alert from Glocker."

		if err := sendEmail(config, subject, body); err != nil {
			log.Printf("Failed to send config reload accountability email: %v", err)
		} else {
			log.Println("Config reload accountability email sent")
		}
	}

	conn.Write([]byte("COMPLETED: Configuration reloaded successfully\n"))
}

func processUninstallRequestWithCompletion(config *Config, reason string, conn net.Conn) {
	log.Printf("Processing uninstall request with reason: %s", reason)

	// Disable signal handlers to prevent extra emails during uninstall
	signal.Reset()
	log.Println("Signal handlers disabled for uninstall process")

	// Send accountability email first, before disabling anything
	if config.Accountability.Enabled {
		subject := "GLOCKER ALERT: Uninstallation Started"
		body := fmt.Sprintf("Glocker uninstallation was requested at %s.\n\n", time.Now().Format("2006-01-02 15:04:05"))
		body += fmt.Sprintf("Uninstall reason: %s\n\n", reason)
		body += "All protections will be removed and original settings restored.\n\n"
		body += "This is an automated alert from Glocker."

		if err := sendEmail(config, subject, body); err != nil {
			log.Printf("Failed to send uninstall accountability email: %v", err)
		} else {
			log.Println("Uninstall accountability email sent")
		}
	}

	// Disable tamper detection to prevent spam emails during uninstall
	log.Println("Disabling tamper detection for uninstall...")
	config.TamperDetection.Enabled = false

	// Give a moment for the email to be sent
	time.Sleep(2 * time.Second)

	// Restore all system changes
	log.Println("Starting uninstall process...")
	restoreSystemChanges(config)

	// Make service file mutable
	log.Println("Making service file mutable...")
	servicePath := "/etc/systemd/system/glocker.service"
	if err := exec.Command("chattr", "-i", servicePath).Run(); err != nil {
		log.Printf("   Warning: couldn't make service file mutable: %v", err)
	} else {
		log.Println("✓ Service file made mutable")
	}

	// Make binary mutable
	log.Println("Making glocker binary mutable...")
	if err := exec.Command("chattr", "-i", INSTALL_PATH).Run(); err != nil {
		log.Printf("   Warning: couldn't make glocker binary mutable: %v", err)
	} else {
		log.Println("✓ Glocker binary made mutable")
	}

	// Send completion signal to process ONE
	conn.Write([]byte("COMPLETED: System changes restored, files made mutable\n"))
	log.Println("Uninstall preparation complete. Exiting...")

	// Exit the process
	os.Exit(0)
}

func restoreSystemChanges(config *Config) {
	log.Println("╔════════════════════════════════════════════════╗")
	log.Println("║           RESTORING SYSTEM CHANGES             ║")
	log.Println("╚════════════════════════════════════════════════╝")
	log.Println()

	// Clean up firewall rules
	log.Println("Clearing firewall rules...")
	clearCmd := `iptables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | xargs -r -L1 iptables`
	if err := exec.Command("bash", "-c", clearCmd).Run(); err != nil {
		log.Printf("   Warning: couldn't clear IPv4 rules: %v", err)
	} else {
		log.Println("✓ IPv4 firewall rules cleared")
	}

	clearCmd6 := `ip6tables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | xargs -r -L1 ip6tables`
	if err := exec.Command("bash", "-c", clearCmd6).Run(); err != nil {
		log.Printf("   Warning: couldn't clear IPv6 rules: %v", err)
	} else {
		log.Println("✓ IPv6 firewall rules cleared")
	}

	// Clean up hosts file
	log.Println("Restoring hosts file...")
	if err := cleanupHostsFile(config); err != nil {
		log.Printf("   Warning: couldn't clean hosts file: %v", err)
	} else {
		log.Println("✓ Hosts file restored")
	}

	// Restore sudoers
	if config.Sudoers.Enabled {
		log.Println("Restoring sudoers configuration...")
		if err := restoreSudoers(config); err != nil {
			log.Printf("   Warning: couldn't restore sudoers: %v", err)
		} else {
			log.Println("✓ Sudoers configuration restored")
		}
	}

	// Remove sudoers backup
	if err := os.Remove(SUDOERS_BACKUP); err != nil {
		log.Printf("   Warning: couldn't remove sudoers backup: %v", err)
	} else {
		log.Println("✓ Sudoers backup removed")
	}

	// Make config file mutable and remove it
	log.Println("Removing config file...")
	if err := exec.Command("chattr", "-i", GLOCKER_CONFIG_FILE).Run(); err != nil {
		log.Printf("   Warning: couldn't make config file mutable: %v", err)
	}
	if err := os.Remove(GLOCKER_CONFIG_FILE); err != nil {
		log.Printf("   Warning: couldn't remove config file: %v", err)
	} else {
		log.Println("✓ Config file removed")
	}

	// Remove config directory if empty
	configDir := filepath.Dir(GLOCKER_CONFIG_FILE)
	if err := os.Remove(configDir); err != nil {
		log.Printf("   Warning: couldn't remove config directory (may not be empty): %v", err)
	} else {
		log.Println("✓ Config directory removed")
	}

	// Remove socket file
	if err := os.Remove(GLOCKER_SOCK); err != nil {
		log.Printf("   Warning: couldn't remove socket file: %v", err)
	} else {
		log.Println("✓ Socket file removed")
	}

	log.Println("✓ System changes restored successfully")
}

func broadcastKeywordUpdate(config *Config) {
	// Combine content keywords with URL keywords
	combinedContentKeywords := make([]string, 0, len(config.ExtensionKeywords.ContentKeywords)+len(config.ExtensionKeywords.URLKeywords))
	combinedContentKeywords = append(combinedContentKeywords, config.ExtensionKeywords.ContentKeywords...)
	combinedContentKeywords = append(combinedContentKeywords, config.ExtensionKeywords.URLKeywords...)

	keywords := map[string]interface{}{
		"url_keywords":     config.ExtensionKeywords.URLKeywords,
		"content_keywords": combinedContentKeywords,
		"whitelist":        config.ExtensionKeywords.Whitelist,
	}

	keywordsJSON, err := json.Marshal(keywords)
	if err != nil {
		slog.Debug("Failed to marshal keywords for broadcast", "error", err)
		return
	}

	sseClientsMutex.RLock()
	clientCount := len(sseClients)
	sseClientsMutex.RUnlock()

	if clientCount == 0 {
		slog.Debug("No SSE clients to broadcast to")
		return
	}

	slog.Debug("Broadcasting keyword update to SSE clients", "client_count", clientCount)

	sseClientsMutex.RLock()
	for i, clientChan := range sseClients {
		select {
		case clientChan <- string(keywordsJSON):
			slog.Debug("Sent keyword update to SSE client", "client_index", i)
		default:
			slog.Debug("SSE client channel full, skipping", "client_index", i)
		}
	}
	sseClientsMutex.RUnlock()
}

func recordViolation(config *Config, violationType, host, url string) {
	if !config.ViolationTracking.Enabled {
		return
	}

	violation := Violation{
		Timestamp: time.Now(),
		Host:      host,
		URL:       url,
		Type:      violationType,
	}

	violationsMutex.Lock()
	violations = append(violations, violation)
	violationsMutex.Unlock()

	slog.Debug("Recorded violation", "type", violationType, "host", host, "url", url)
	log.Printf("VIOLATION RECORDED: %s - %s (%s)", violationType, host, url)

	// Check if we've exceeded the threshold
	go checkViolationThreshold(config)
}

func checkViolationThreshold(config *Config) {
	if !config.ViolationTracking.Enabled {
		return
	}

	now := time.Now()
	recentCount := countRecentViolations(config, now)

	slog.Debug("Checking violation threshold", "recent_count", recentCount, "max_violations", config.ViolationTracking.MaxViolations)

	if recentCount >= config.ViolationTracking.MaxViolations {
		log.Printf("VIOLATION THRESHOLD EXCEEDED: %d/%d violations in last %d minutes",
			recentCount, config.ViolationTracking.MaxViolations, config.ViolationTracking.TimeWindowMinutes)

		// Send desktop notification
		sendNotification(config, "Glocker Alert", 
			fmt.Sprintf("Violation threshold exceeded: %d/%d", recentCount, config.ViolationTracking.MaxViolations),
			"critical", "dialog-warning")

		// Execute the configured command
		if config.ViolationTracking.Command != "" {
			executeViolationCommand(config, recentCount)
		}

		// Send accountability email
		if config.Accountability.Enabled {
			sendViolationEmail(config, recentCount)
		}

		// Do not reset violations here - they will only be reset during daily reset
		log.Printf("Violation command executed - violations will continue to trigger until daily reset")
	}
}

func countRecentViolations(config *Config, now time.Time) int {
	if !config.ViolationTracking.Enabled {
		return 0
	}

	timeWindow := time.Duration(config.ViolationTracking.TimeWindowMinutes) * time.Minute
	cutoff := now.Add(-timeWindow)

	count := 0
	for _, violation := range violations {
		if violation.Timestamp.After(cutoff) {
			count++
		}
	}

	return count
}

func executeViolationCommand(config *Config, violationCount int) {
	if config.ViolationTracking.Command == "" {
		return
	}

	slog.Debug("Executing violation command", "command", config.ViolationTracking.Command, "violation_count", violationCount)

	// Split command into parts for proper execution
	parts := strings.Fields(config.ViolationTracking.Command)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	// Set environment variables with violation information
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GLOCKER_VIOLATION_COUNT=%d", violationCount),
		fmt.Sprintf("GLOCKER_MAX_VIOLATIONS=%d", config.ViolationTracking.MaxViolations),
		fmt.Sprintf("GLOCKER_TIME_WINDOW=%d", config.ViolationTracking.TimeWindowMinutes),
		"GLOCKER_VIOLATION_TRIGGERED=true",
		"DISPLAY=:0",
	)

	// Create a new session for the violation command (needed for screen locking)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		slog.Debug("Failed to execute violation command", "error", err, "stdout_stderr", string(output), "command", config.ViolationTracking.Command)
		log.Printf("Failed to execute violation command: %v", err)
		log.Printf("Command output: %s", string(output))
		log.Printf("Environment variables:")
		for _, env := range cmd.Env {
			log.Printf("  %s", env)
		}
	} else {
		slog.Debug("Violation command executed successfully", "command", config.ViolationTracking.Command, "output", string(output))
		log.Printf("Violation command executed successfully: %s", config.ViolationTracking.Command)
	}
}

func sendViolationEmail(config *Config, violationCount int) {
	subject := "GLOCKER ALERT: Violation Threshold Exceeded"
	body := fmt.Sprintf("The violation threshold has been exceeded at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
	body += fmt.Sprintf("Violations: %d/%d (in last %d minutes)\n",
		violationCount, config.ViolationTracking.MaxViolations, config.ViolationTracking.TimeWindowMinutes)
	body += fmt.Sprintf("Command executed: %s\n\n", config.ViolationTracking.Command)

	// Add recent violations details
	violationsMutex.RLock()
	body += "Recent violations:\n"
	now := time.Now()
	timeWindow := time.Duration(config.ViolationTracking.TimeWindowMinutes) * time.Minute
	cutoff := now.Add(-timeWindow)

	for _, violation := range violations {
		if violation.Timestamp.After(cutoff) {
			body += fmt.Sprintf("  - %s: %s (%s) at %s\n",
				violation.Type, violation.Host, violation.URL, violation.Timestamp.Format("15:04:05"))
		}
	}
	violationsMutex.RUnlock()

	body += "\nThis is an automated alert from Glocker."

	if err := sendEmail(config, subject, body); err != nil {
		log.Printf("Failed to send violation accountability email: %v", err)
	}
}

func monitorViolations(config *Config) {
	if !config.ViolationTracking.Enabled || !config.ViolationTracking.ResetDaily {
		return
	}

	// Initialize last reset time
	lastViolationReset = time.Now()

	// Parse reset time
	resetTime := config.ViolationTracking.ResetTime
	if resetTime == "" {
		resetTime = "00:00" // Default to midnight
	}

	slog.Debug("Starting violation monitoring", "reset_daily", config.ViolationTracking.ResetDaily, "reset_time", resetTime)

	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		currentTime := now.Format("15:04")

		// Check if it's time to reset violations
		if currentTime == resetTime && now.Sub(lastViolationReset) > 23*time.Hour {
			violationsMutex.Lock()
			oldCount := len(violations)
			violations = []Violation{}
			lastViolationReset = now
			violationsMutex.Unlock()

			if oldCount > 0 {
				log.Printf("Daily violation reset: cleared %d violations at %s", oldCount, resetTime)
			}
		}
	}
}

func handleBlockedPageRequest(w http.ResponseWriter, r *http.Request) {
	// Only accept GET requests
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Get parameters from the query string
	domain := r.URL.Query().Get("domain")
	matchedDomain := r.URL.Query().Get("matched")
	originalURL := r.URL.Query().Get("url")
	reason := r.URL.Query().Get("reason")

	// Set defaults if parameters are missing
	if domain == "" {
		domain = "this site"
	}
	if matchedDomain == "" {
		matchedDomain = domain
	}
	if reason == "" {
		reason = "This content has been blocked by Glocker."
	}

	// Set content type
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	// Add original URL info if provided
	originalURLInfo := ""
	if originalURL != "" {
		originalURLInfo = fmt.Sprintf(`<p class="matched">Original URL: %s</p>`, originalURL)
	}

	// Generate the blocked page HTML
	blockedPage := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <title>Site Blocked - Glocker</title>
    <style>
        body { 
            font-family: Arial, sans-serif; 
            text-align: center; 
            margin-top: 100px; 
            background-color: #f0f0f0; 
        }
        .container { 
            max-width: 600px; 
            margin: 0 auto; 
            padding: 20px; 
            background-color: white; 
            border-radius: 10px; 
            box-shadow: 0 4px 6px rgba(0,0,0,0.1); 
        }
        h1 { 
            color: #d32f2f; 
        }
        p { 
            color: #666; 
            line-height: 1.6; 
        }
        .domain { 
            font-weight: bold; 
            color: #1976d2; 
            background-color: #e3f2fd; 
            padding: 8px 12px; 
            border-radius: 5px; 
            margin: 10px 0; 
            display: inline-block;
        }
        .matched { 
            font-size: 0.9em; 
            color: #888; 
            margin: 10px 0; 
        }
        .time { 
            color: #888; 
            font-size: 0.9em; 
        }
        .command-hint {
            background-color: #f8f9fa;
            border-left: 4px solid #1976d2;
            padding: 15px;
            margin: 20px 0;
            text-align: left;
            border-radius: 4px;
        }
        .command {
            font-family: 'Courier New', monospace;
            background-color: #e9ecef;
            padding: 5px 8px;
            border-radius: 3px;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚫 Site Blocked</h1>
        <p>Access to <span class="domain">%s</span> has been blocked by Glocker.</p>
        <p class="matched">Matched blocking rule: %s</p>
        %s
        <p>This site is currently in your blocked domains list.</p>
        <p class="time">Blocked at: %s</p>
    </div>
</body>
</html>`, domain, matchedDomain, originalURLInfo, time.Now().Format("2006-01-02 15:04:05"))

	w.Write([]byte(blockedPage))
}

func sendEmail(config *Config, subject, body string) error {
	if !config.Accountability.Enabled {
		return nil
	}

	// Skip sending emails in dev mode
	if config.Dev {
		log.Printf("DEV MODE: Skipping email send - Subject: %s, Body: %s", subject, body)
		return nil
	}

	from := config.Accountability.FromEmail
	to := config.Accountability.PartnerEmail
	apiKey := config.Accountability.ApiKey
	log.Printf("Sending email from %s to %s subject %s", from, to, subject)

	mg := mailgun.NewMailgun("noufalibrahim.name", apiKey)

	// Convert plain text body to HTML
	htmlBody := generateHTMLEmail(subject, body)

	mail := mailgun.NewMessage(
		from,
		subject,
		body, // Keep plain text as fallback
		to,
	)

	// Set HTML content
	mail.SetHTML(htmlBody)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	_, _, err := mg.Send(ctx, mail)

	if err != nil {
		return err
	} else {
		return nil
	}
}

func generateHTMLEmail(subject, plainBody string) string {
	// Escape HTML characters in the plain text body
	htmlBody := strings.ReplaceAll(plainBody, "&", "&amp;")
	htmlBody = strings.ReplaceAll(htmlBody, "<", "&lt;")
	htmlBody = strings.ReplaceAll(htmlBody, ">", "&gt;")
	htmlBody = strings.ReplaceAll(htmlBody, "\"", "&quot;")
	htmlBody = strings.ReplaceAll(htmlBody, "'", "&#39;")

	// Convert line breaks to HTML
	htmlBody = strings.ReplaceAll(htmlBody, "\n", "<br>")

	// Determine alert type and styling based on subject
	alertIcon := "ℹ️"
	alertColor := "#1976d2"

	if strings.Contains(strings.ToLower(subject), "tamper") {
		alertIcon = "⚠️"
		alertColor = "#d32f2f"
	} else if strings.Contains(strings.ToLower(subject), "blocked") || strings.Contains(strings.ToLower(subject), "violation") {
		alertIcon = "🚫"
		alertColor = "#f57c00"
	} else if strings.Contains(strings.ToLower(subject), "unblock") {
		alertIcon = "🔓"
		alertColor = "#1976d2"
	} else if strings.Contains(strings.ToLower(subject), "install") {
		alertIcon = "✅"
		alertColor = "#388e3c"
	} else if strings.Contains(strings.ToLower(subject), "termination") {
		alertIcon = "🛡️"
		alertColor = "#d32f2f"
	}

	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s</title>
    <style>
        body { 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
            line-height: 1.6;
            color: #333;
            background-color: #f5f5f5;
            margin: 0;
            padding: 20px;
        }
        .container { 
            max-width: 600px; 
            margin: 0 auto; 
            background-color: white; 
            border-radius: 8px; 
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            overflow: hidden;
        }
        .header {
            background: linear-gradient(135deg, %s, %s);
            color: white;
            padding: 30px 20px;
            text-align: center;
        }
        .header h1 {
            margin: 0;
            font-size: 24px;
            font-weight: 600;
        }
        .header .icon {
            font-size: 48px;
            margin-bottom: 10px;
            display: block;
        }
        .content {
            padding: 30px 20px;
        }
        .alert-box {
            background-color: %s;
            border-left: 4px solid %s;
            padding: 15px 20px;
            margin: 20px 0;
            border-radius: 4px;
        }
        .timestamp {
            background-color: #f8f9fa;
            padding: 10px 15px;
            border-radius: 4px;
            font-family: 'Courier New', monospace;
            font-size: 14px;
            color: #666;
            margin: 15px 0;
        }
        .footer {
            background-color: #f8f9fa;
            padding: 20px;
            text-align: center;
            border-top: 1px solid #e9ecef;
            font-size: 14px;
            color: #666;
        }
        .footer .logo {
            font-weight: bold;
            color: #333;
        }
        pre {
            background-color: #f8f9fa;
            padding: 15px;
            border-radius: 4px;
            overflow-x: auto;
            font-size: 14px;
            border-left: 3px solid %s;
        }
        .highlight {
            background-color: #fff3cd;
            padding: 2px 4px;
            border-radius: 3px;
            font-weight: 500;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <span class="icon">%s</span>
            <h1>Glocker Security Alert</h1>
        </div>
        <div class="content">
            <div class="alert-box">
                <strong>%s</strong>
            </div>
            <div class="timestamp">
                Generated: %s
            </div>
            <div class="message">
                %s
            </div>
        </div>
        <div class="footer">
            <div class="logo">🛡️ Glocker</div>
            <div>Automated Security Monitoring System</div>
            <div style="margin-top: 10px; font-size: 12px;">
                This is an automated message. Please do not reply to this email.
            </div>
        </div>
    </div>
</body>
</html>`,
		subject,                                  // title
		alertColor,                               // gradient start
		adjustColorBrightness(alertColor, -20),   // gradient end (darker)
		adjustColorOpacity(alertColor, 0.1),      // alert box background
		alertColor,                               // alert box border
		alertColor,                               // pre border
		alertIcon,                                // header icon
		subject,                                  // alert box title
		time.Now().Format("2006-01-02 15:04:05"), // timestamp
		htmlBody,                                 // message content
	)
}

func adjustColorBrightness(hexColor string, percent int) string {
	// Simple color adjustment - in a real implementation you might want more sophisticated color manipulation
	if percent < 0 {
		// Darken the color
		switch hexColor {
		case "#d32f2f":
			return "#b71c1c"
		case "#f57c00":
			return "#e65100"
		case "#1976d2":
			return "#1565c0"
		case "#388e3c":
			return "#2e7d32"
		default:
			return "#333333"
		}
	}
	return hexColor
}

func adjustColorOpacity(hexColor string, opacity float64) string {
	// Convert hex color to rgba with opacity
	switch hexColor {
	case "#d32f2f":
		return fmt.Sprintf("rgba(211, 47, 47, %.1f)", opacity)
	case "#f57c00":
		return fmt.Sprintf("rgba(245, 124, 0, %.1f)", opacity)
	case "#1976d2":
		return fmt.Sprintf("rgba(25, 118, 210, %.1f)", opacity)
	case "#388e3c":
		return fmt.Sprintf("rgba(56, 142, 60, %.1f)", opacity)
	default:
		return fmt.Sprintf("rgba(51, 51, 51, %.1f)", opacity)
	}
}

func sendNotification(config *Config, title, message, urgency, icon string) {
	if config.NotificationCommand == "" {
		return
	}

	// Replace placeholders in the command
	cmd := config.NotificationCommand
	cmd = strings.ReplaceAll(cmd, "{title}", title)
	cmd = strings.ReplaceAll(cmd, "{message}", message)
	cmd = strings.ReplaceAll(cmd, "{urgency}", urgency)
	cmd = strings.ReplaceAll(cmd, "{icon}", icon)

	// Split command into parts for proper execution
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	execCmd := exec.Command(parts[0], parts[1:]...)
	execCmd.Env = append(os.Environ(), "DISPLAY=:0")
	
	if err := execCmd.Run(); err != nil {
		slog.Debug("Failed to send notification", "error", err, "command", cmd)
	} else {
		slog.Debug("Notification sent", "title", title, "message", message)
	}
}
