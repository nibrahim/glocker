package main

import (
	"bufio"
	"context"
	"crypto/sha256"
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

	"github.com/mailgun/mailgun-go/v4"
	"gopkg.in/yaml.v3"
)

//go:embed conf/conf.yaml
var configData []byte

const (
	INSTALL_PATH       = "/usr/local/bin/glocker"
	HOSTS_MARKER_START = "### GLOCKER START ###"
	HOSTS_MARKER_END   = "### GLOCKER END ###"
	SUDOERS_PATH       = "/etc/sudoers"
	SUDOERS_BACKUP     = "/etc/sudoers.glocker.backup"
	SUDOERS_MARKER     = "# GLOCKER-MANAGED"
	SYSTEMD_FILE       = "./extras/glocker.service"
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

type Config struct {
	EnableHosts     bool                 `yaml:"enable_hosts"`
	EnableFirewall  bool                 `yaml:"enable_firewall"`
	Domains         []Domain             `yaml:"domains"`
	HostsPath       string               `yaml:"hosts_path"`
	SelfHeal        bool                 `yaml:"enable_self_healing"`
	EnforceInterval int                  `yaml:"enforce_interval_seconds"`
	Sudoers         SudoersConfig        `yaml:"sudoers"`
	TamperDetection TamperConfig         `yaml:"tamper_detection"`
	Accountability  AccountabilityConfig `yaml:"accountability"`
	MindfulDelay    int                  `yaml:"mindful_delay"`
	TempUnblockTime int                  `yaml:"temp_unblock_time"`
}

func main() {
	dryRun := flag.Bool("dry-run", false, "Show what would be done without making changes")
	enforce := flag.Bool("enforce", false, "Run enforcement loop (runs continuously)")
	once := flag.Bool("once", false, "Run enforcement once and exit")
	install := flag.Bool("install", false, "Install Glocker")
	uninstall := flag.Bool("uninstall", false, "Uninstall Glocker and revert all changes")
	blockHosts := flag.String("block", "", "Comma-separated list of hosts to add to always block list")
	deleteHosts := flag.String("delete", "", "Comma-separated list of hosts to temporarily unblock")
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

	if *install {
		if !runningAsRoot() {
			log.Fatal("Program should run as root for installation.")
		}
		installGlocker(&config)
		return
	}

	if *uninstall {
		if !runningAsRoot() {
			log.Fatal("Program should run as root for uninstallation.")
		}
		// Check if glocker is actually installed
		if _, err := os.Stat(INSTALL_PATH); os.IsNotExist(err) {
			log.Fatal("Glocker is not installed. Nothing to uninstall.")
		}
		uninstallGlocker(&config)
		return
	}

	if *blockHosts != "" {
		if !runningAsRoot() {
			log.Fatal("Program should run as root for blocking hosts.")
		}
		blockHostsFromFlag(&config, *blockHosts)
		return
	}

	if *deleteHosts != "" {
		if !runningAsRoot() {
			log.Fatal("Program should run as root for unblocking hosts.")
		}
		deleteHostsFromFlag(&config, *deleteHosts)
		return
	}

	if *dryRun {
		log.Println("=== DRY RUN MODE ===")
		printConfig(&config)
		runOnce(&config, true)
		return
	}

	if *once {
		if !runningAsRoot() {
			log.Fatal("Program should run as root for running once.")
		}
		runOnce(&config, false)
		return
	}

	if *enforce {
		if !runningAsRoot() {
			log.Fatal("Program should run as root for running once.")
		}
		log.Println("Starting enforcement loop...")
		log.Printf("Enforcement interval: %d seconds", config.EnforceInterval)

		// Start tamper detection in background if enabled
		if config.TamperDetection.Enabled {
			// Capture initial checksums before starting monitoring
			filesToMonitor := []string{
				INSTALL_PATH,
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

}

func runOnce(config *Config, dryRun bool) {
	now := time.Now()
	blockedDomains := getDomainsToBlock(config, now)

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

func installGlocker(config *Config) {
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

	// Create sudoers backup if sudoers management is enabled
	if config.Sudoers.Enabled {
		createSudoersBackup()
	}

	log.Println("Installation complete!")
}

func runningAsRoot() bool {
	if os.Geteuid() != 0 {
		return false
	} else {
		return true
	}
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

func uninstallGlocker(config *Config) {
	log.Println("╔════════════════════════════════════════════════╗")
	log.Println("║              GLOCKER UNINSTALL                 ║")
	log.Println("╚════════════════════════════════════════════════╝")
	log.Println()

	fmt.Println("📝 MINDFUL UNINSTALL PROCESS")
	fmt.Println()
	fmt.Println("To proceed with uninstallation, please type the following quote")
	fmt.Println("EXACTLY as shown (including punctuation and capitalization):")
	fmt.Println()

	// Perform mindful delay
	mindfulDelay(config)

	// Stop and disable service
	exec.Command("systemctl", "stop", "glocker.service").Run()
	exec.Command("systemctl", "disable", "glocker.service").Run()

	// Clean up firewall rules
	clearCmd := `iptables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | xargs -r -L1 iptables`
	if err := exec.Command("bash", "-c", clearCmd).Run(); err != nil {
		log.Printf("   Warning: couldn't clear IPv4 rules: %v", err)
	}

	clearCmd6 := `ip6tables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | xargs -r -L1 ip6tables`
	exec.Command("bash", "-c", clearCmd6).Run()

	// Clean up hosts file
	cleanupHostsFile(config)

	// Restore sudoers
	if config.Sudoers.Enabled {
		restoreSudoers(config)
	}

	// Remove service file
	servicePath := "/etc/systemd/system/glocker.service"
	exec.Command("chattr", "-i", servicePath).Run()
	os.Remove(servicePath)
	exec.Command("systemctl", "daemon-reload").Run()

	// Remove binary
	exec.Command("chattr", "-i", INSTALL_PATH).Run()
	if err := os.Remove(INSTALL_PATH); err != nil {
		log.Printf("Error removing glocker binary: %v", err)
	} else {
		log.Println("✓ Glocker binary removed")
	}

	// Remove sudoers backup
	if err := os.Remove(SUDOERS_BACKUP); err != nil {
		log.Printf("Error removing sudoers backup: %v", err)
	}

	log.Println("")
	log.Println("🎉 Glocker has been completely uninstalled!")
	log.Println("   All protections have been removed and original settings restored.")
}

func mindfulDelay(config *Config) {
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
	inBlockSection := false

	// Remove glocker block section
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

func getDomainsToBlock(config *Config, now time.Time) []string {
	var blocked []string
	currentDay := now.Weekday().String()[:3] // Mon, Tue, etc.
	currentTime := now.Format("15:04")

	for _, domain := range config.Domains {
		// Check if domain is temporarily unblocked
		if isTempUnblocked(domain.Name, now) {
			continue
		}

		if domain.AlwaysBlock {
			blocked = append(blocked, domain.Name)
			continue
		}

		// Check time windows
		for _, window := range domain.TimeWindows {
			if !slices.Contains(window.Days, currentDay) {
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

func isTempUnblocked(domain string, now time.Time) bool {
	for _, unblock := range tempUnblocks {
		if unblock.Domain == domain && now.Before(unblock.ExpiresAt) {
			return true
		}
	}
	return false
}

func updateHosts(config *Config, domains []string, dryRun bool) error {
	hostsPath := config.HostsPath

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

	// Reconstruct file content preserving original structure
	var newContent string

	// Join original content (excluding our block section)
	if len(newLines) > 0 {
		// Remove any trailing empty lines from original content
		for len(newLines) > 0 && strings.TrimSpace(newLines[len(newLines)-1]) == "" {
			newLines = newLines[:len(newLines)-1]
		}
		newContent = strings.Join(newLines, "\n")
		if newContent != "" {
			newContent += "\n"
		}
	}

	// Add our block section
	if len(blockSection) > 0 {
		if newContent != "" {
			newContent += "\n" // Empty line before our block
		}
		newContent += strings.Join(blockSection, "\n") + "\n"
	}

	if dryRun {
		return nil
	}

	// Remove immutable flag temporarily
	exec.Command("chattr", "-i", hostsPath).Run()

	// Write file
	if err := os.WriteFile(hostsPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("writing hosts file: %w", err)
	}

	// Set immutable flag
	exec.Command("chattr", "+i", hostsPath).Run()

	// Update checksum after legitimate change
	updateChecksum(hostsPath)

	return nil
}

func updateFirewall(domains []string, dryRun bool) error {
	if dryRun {
		return nil
	}

	// Clear old rules with our marker
	clearCmd := `iptables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | while read rule; do iptables $rule 2>/dev/null; done`
	exec.Command("bash", "-c", clearCmd).Run()

	// Also clear ip6tables rules
	clearCmd6 := `ip6tables -S OUTPUT | grep 'GLOCKER-BLOCK' | sed 's/-A/-D/' | while read rule; do ip6tables $rule 2>/dev/null; done`
	exec.Command("bash", "-c", clearCmd6).Run()

	totalIPs := 0
	for _, domain := range domains {
		// Resolve and block IPv4 addresses
		ips := resolveIPs(domain, "A")
		for _, ip := range ips {
			cmd := exec.Command("iptables", "-I", "OUTPUT", "-d", ip,
				"-j", "REJECT", "--reject-with", "icmp-host-unreachable",
				"-m", "comment", "--comment", "GLOCKER-BLOCK")

			if err := cmd.Run(); err == nil {
				totalIPs++
			}
		}

		// Resolve and block IPv6 addresses
		ips6 := resolveIPs(domain, "AAAA")
		for _, ip := range ips6 {
			cmd := exec.Command("ip6tables", "-I", "OUTPUT", "-d", ip,
				"-j", "REJECT", "--reject-with", "icmp6-adm-prohibited",
				"-m", "comment", "--comment", "GLOCKER-BLOCK")

			if err := cmd.Run(); err == nil {
				totalIPs++
			}
		}
	}

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

// Global variables for tamper detection
var (
	globalChecksums      []FileChecksum
	globalFilesToMonitor []string
	globalConfig         *Config
	tempUnblocks         []TempUnblock
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
		if strings.Contains(line, HOSTS_MARKER_END) {
			glockerLines = append(glockerLines, line)
			inGlockerSection = false
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

func deleteHostsFromFlag(config *Config, hostsStr string) {
	hosts := strings.Split(hostsStr, ",")
	var validHosts []string

	// Clean and validate hosts
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host != "" {
			validHosts = append(validHosts, host)
		}
	}

	if len(validHosts) == 0 {
		log.Fatal("No valid hosts provided")
	}

	fmt.Println("🔓 MINDFUL UNBLOCK PROCESS")
	fmt.Println()
	fmt.Printf("You are requesting to temporarily unblock: %s\n", strings.Join(validHosts, ", "))
	fmt.Println()
	fmt.Println("To proceed with temporary unblocking, please type the following quote")
	fmt.Println("EXACTLY as shown (including punctuation and capitalization):")
	fmt.Println()

	// Mindful unblock process
	mindfulDelay(config)

	// Set temporary unblock time (default 30 minutes)
	unblockDuration := config.TempUnblockTime
	if unblockDuration == 0 {
		unblockDuration = 30
	}

	expiresAt := time.Now().Add(time.Duration(unblockDuration) * time.Minute)

	log.Printf("Temporarily unblocking %d hosts for %d minutes: %v", len(validHosts), unblockDuration, validHosts)

	// Add to temporary unblock list
	for _, host := range validHosts {
		tempUnblocks = append(tempUnblocks, TempUnblock{
			Domain:    host,
			ExpiresAt: expiresAt,
		})
	}

	// Send accountability email
	if config.Accountability.Enabled {
		subject := "GLOCKER ALERT: Domains Temporarily Unblocked"
		body := fmt.Sprintf("The following domains were temporarily unblocked at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
		for _, host := range validHosts {
			body += fmt.Sprintf("  - %s (expires at %s)\n", host, expiresAt.Format("2006-01-02 15:04:05"))
		}
		body += fmt.Sprintf("\nThey will be automatically re-blocked after %d minutes.\n", unblockDuration)
		body += "\nThis is an automated alert from Glocker."

		if err := sendEmail(config, subject, body); err != nil {
			log.Printf("Failed to send accountability email: %v", err)
		}
	}

	// Apply the unblocking immediately
	log.Println("Applying temporary unblocks...")
	runOnce(config, false)

	// Schedule re-blocking
	go func() {
		time.Sleep(time.Duration(unblockDuration) * time.Minute)
		log.Printf("Re-blocking expired domains: %v", validHosts)

		// Remove from temp unblock list
		var remaining []TempUnblock
		for _, unblock := range tempUnblocks {
			found := false
			for _, host := range validHosts {
				if unblock.Domain == host {
					found = true
					break
				}
			}
			if !found {
				remaining = append(remaining, unblock)
			}
		}
		tempUnblocks = remaining

		// Re-apply blocking
		runOnce(config, false)
		log.Printf("Domains have been re-blocked: %v", validHosts)
	}()

	log.Printf("Hosts have been temporarily unblocked for %d minutes!", unblockDuration)
}

func blockHostsFromFlag(config *Config, hostsStr string) {
	hosts := strings.Split(hostsStr, ",")
	var validHosts []string

	// Clean and validate hosts
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host != "" {
			validHosts = append(validHosts, host)
		}
	}

	if len(validHosts) == 0 {
		log.Fatal("No valid hosts provided")
	}

	log.Printf("Adding %d hosts to always block list: %v", len(validHosts), validHosts)

	// Add hosts to config as always blocked domains
	for _, host := range validHosts {
		// Check if domain already exists
		found := false
		for i, domain := range config.Domains {
			if domain.Name == host {
				// Update existing domain to always block
				config.Domains[i].AlwaysBlock = true
				config.Domains[i].TimeWindows = nil // Clear time windows since it's always blocked
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
			}
			config.Domains = append(config.Domains, newDomain)
			log.Printf("Added new domain %s to always block", host)
		}
	}

	// Apply the blocking immediately
	log.Println("Applying blocks...")
	runOnce(config, false)
	
	// Update checksum for hosts file after legitimate changes
	if globalConfig != nil {
		updateChecksum(config.HostsPath)
		log.Println("Updated checksum for hosts file after blocking new domains")
	}
	
	log.Println("Hosts have been blocked successfully!")
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

	fmt.Printf("Domains: %d total (%d always blocked, %d time-based)\n",
		len(config.Domains), alwaysBlockCount, timeBasedCount)

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
}

func sendEmail(config *Config, subject, body string) error {
	if !config.Accountability.Enabled {
		return nil
	}

	from := config.Accountability.FromEmail
	to := config.Accountability.PartnerEmail
	apiKey := config.Accountability.ApiKey
	log.Printf("Sending email from %s to %s subject %s : %s", from, to, subject, body)

	mg := mailgun.NewMailgun("noufalibrahim.name", apiKey)

	mail := mailgun.NewMessage(
		from,
		subject,
		body,
		to,
	)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	_, _, err := mg.Send(ctx, mail)

	if err != nil {
		return err
	} else {
		return nil
	}

}
