package install

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"glocker/config"
)

// RestoreSystemChanges removes all glocker modifications and restores the system to its original state.
// This includes cleaning firewall rules, hosts file, sudoers, Firefox extension, and removing config files.
func RestoreSystemChanges(cfg *config.Config) error {
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
	if err := cleanupHostsFile(cfg); err != nil {
		log.Printf("   Warning: couldn't clean hosts file: %v", err)
	} else {
		log.Println("✓ Hosts file restored")
	}

	// Restore sudoers
	if cfg.Sudoers.Enabled {
		log.Println("Restoring sudoers configuration...")
		if err := restoreSudoers(cfg); err != nil {
			log.Printf("   Warning: couldn't restore sudoers: %v", err)
		} else {
			log.Println("✓ Sudoers configuration restored")
		}
	}

	// Remove sudoers backup
	if err := os.Remove(config.SudoersBackup); err != nil {
		log.Printf("   Warning: couldn't remove sudoers backup: %v", err)
	} else {
		log.Println("✓ Sudoers backup removed")
	}

	// Clean up Firefox extension
	log.Println("Removing Firefox extension...")
	if err := UninstallFirefoxExtension(); err != nil {
		log.Printf("   Warning: couldn't remove Firefox extension: %v", err)
	} else {
		log.Println("✓ Firefox extension removed")
	}

	// Make config file mutable and remove it
	log.Println("Removing config file...")
	if err := exec.Command("chattr", "-i", config.GlockerConfigFile).Run(); err != nil {
		log.Printf("   Warning: couldn't make config file mutable: %v", err)
	}
	if err := os.Remove(config.GlockerConfigFile); err != nil {
		log.Printf("   Warning: couldn't remove config file: %v", err)
	} else {
		log.Println("✓ Config file removed")
	}

	// Remove config directory if empty
	configDir := filepath.Dir(config.GlockerConfigFile)
	if err := os.Remove(configDir); err != nil {
		log.Printf("   Warning: couldn't remove config directory (may not be empty): %v", err)
	} else {
		log.Println("✓ Config directory removed")
	}

	// Remove socket file
	socketPath := "/tmp/glocker.sock"
	if err := os.Remove(socketPath); err != nil {
		log.Printf("   Warning: couldn't remove socket file: %v", err)
	} else {
		log.Println("✓ Socket file removed")
	}

	log.Println("✓ System changes restored successfully")
	return nil
}

// cleanupHostsFile removes the glocker section from the hosts file.
func cleanupHostsFile(cfg *config.Config) error {
	hostsPath := cfg.HostsPath
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
		if strings.Contains(line, config.HostsMarkerStart) {
			// Stop processing once we hit the start marker
			break
		}
		newLines = append(newLines, line)
	}

	// Write cleaned content
	newContent := strings.Join(newLines, "\n")
	return os.WriteFile(hostsPath, []byte(newContent), 0644)
}

// restoreSudoers restores the sudoers file from backup or replaces blocked line with allowed line.
func restoreSudoers(cfg *config.Config) error {
	// Check if backup exists
	if _, err := os.Stat(config.SudoersBackup); os.IsNotExist(err) {
		// No backup exists, replace blocked line with allowed line
		return replaceBlockedWithAllowed(cfg)
	}

	// Restore from backup
	backupContent, err := os.ReadFile(config.SudoersBackup)
	if err != nil {
		return fmt.Errorf("reading sudoers backup: %w", err)
	}

	// Write to temporary file for validation
	tmpFile := config.SudoersPath + ".tmp"
	if err := os.WriteFile(tmpFile, backupContent, 0440); err != nil {
		return fmt.Errorf("writing temporary sudoers file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Validate with visudo
	cmd := exec.Command("visudo", "-c", "-f", tmpFile)
	if err := cmd.Run(); err != nil {
		// If backup is invalid, fall back to replacing blocked with allowed
		log.Printf("Backup sudoers file is invalid, replacing blocked line with allowed instead")
		return replaceBlockedWithAllowed(cfg)
	}

	// Validation passed, restore the backup
	if err := os.Rename(tmpFile, config.SudoersPath); err != nil {
		return fmt.Errorf("restoring sudoers file: %w", err)
	}

	// Ensure correct permissions
	return os.Chmod(config.SudoersPath, 0440)
}

// replaceBlockedWithAllowed replaces the blocked sudoers line with the allowed line.
func replaceBlockedWithAllowed(cfg *config.Config) error {
	// Read current sudoers file
	content, err := os.ReadFile(config.SudoersPath)
	if err != nil {
		return fmt.Errorf("reading sudoers file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	found := false

	// Look for our managed line and replace blocked with allowed
	for _, line := range lines {
		if strings.Contains(line, config.SudoersMarker) {
			// Replace with allowed line
			newLines = append(newLines, cfg.Sudoers.AllowedSudoersLine+" "+config.SudoersMarker)
			found = true
		} else {
			newLines = append(newLines, line)
		}
	}

	// If we didn't find our managed line, add the allowed line
	if !found {
		newLines = append(newLines, cfg.Sudoers.AllowedSudoersLine+" "+config.SudoersMarker)
	}

	newContent := strings.Join(newLines, "\n")

	// Write to temporary file for validation
	tmpFile := config.SudoersPath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(newContent), 0440); err != nil {
		return fmt.Errorf("writing temporary sudoers file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Validate with visudo
	cmd := exec.Command("visudo", "-c", "-f", tmpFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudoers validation failed: %w", err)
	}

	// Validation passed, replace the sudoers file
	if err := os.Rename(tmpFile, config.SudoersPath); err != nil {
		return fmt.Errorf("replacing sudoers file: %w", err)
	}

	// Ensure correct permissions
	return os.Chmod(config.SudoersPath, 0440)
}
