package enforcement

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"glocker/internal/config"
	"glocker/internal/state"
)

// EnforcementState tracks the current state of enforcement to avoid unnecessary rewrites.
type EnforcementState struct {
	mu sync.RWMutex

	// Hosts file state
	hostsChecksum     string
	expectedHostsHash string
	lastBlockedCount  int

	// Time window state - tracks which domains are in blocking period
	lastTimeWindowState map[string]bool // domain -> was blocked by time window

	// Temp unblock state
	lastTempUnblockCount int

	// Sudoers state
	lastSudoersLocked bool

	// Last enforcement time
	lastEnforcement time.Time

	// Config checksum for detecting config changes
	configChecksum string
}

var (
	enforcementState = &EnforcementState{
		lastTimeWindowState: make(map[string]bool),
	}
)

// InitialEnforcement performs the initial full enforcement on daemon startup.
// This builds the hosts file, applies all protections, and stores the initial state.
func InitialEnforcement(cfg *config.Config) {
	now := time.Now()
	log.Printf("Performing initial enforcement at %s", now.Format("2006-01-02 15:04:05"))

	// Clean up expired temporary unblocks
	CleanupExpiredUnblocks(now)

	// Get domains to block
	blockedDomains := GetDomainsToBlock(cfg, now)
	log.Printf("Initial enforcement: %d domains to block", len(blockedDomains))

	// Build and write hosts file
	if cfg.EnableHosts {
		if err := UpdateHosts(cfg, blockedDomains, false); err != nil {
			log.Printf("ERROR updating hosts: %v", err)
		} else {
			// Store the expected hash of the hosts file
			if hash, err := computeFileChecksum(cfg.HostsPath); err == nil {
				enforcementState.mu.Lock()
				enforcementState.expectedHostsHash = hash
				enforcementState.lastBlockedCount = len(blockedDomains)
				enforcementState.mu.Unlock()
				log.Printf("Hosts file checksum stored: %s", hash[:16])
			}
		}
	}

	// Update firewall
	if cfg.EnableFirewall {
		if err := UpdateFirewall(blockedDomains, false); err != nil {
			log.Printf("ERROR updating firewall: %v", err)
		}
	}

	// Update sudoers
	if cfg.Sudoers.Enabled {
		if err := UpdateSudoers(cfg, now, false, false); err != nil {
			log.Printf("ERROR updating sudoers: %v", err)
		}
		// Store sudoers lock state
		enforcementState.mu.Lock()
		enforcementState.lastSudoersLocked = !isSudoersAllowed(cfg, now)
		enforcementState.mu.Unlock()
	}

	// Self-heal check
	if cfg.SelfHeal {
		SelfHeal(cfg)
	}

	// Store time window state for all domains
	enforcementState.mu.Lock()
	enforcementState.lastTimeWindowState = buildTimeWindowState(cfg, now)
	enforcementState.lastTempUnblockCount = len(state.GetTempUnblocks())
	enforcementState.lastEnforcement = now
	enforcementState.mu.Unlock()

	log.Println("Initial enforcement completed")
}

// EnforcementCheck performs a lightweight check and only applies changes if needed.
// This is called periodically and avoids rewriting files unless something changed.
func EnforcementCheck(cfg *config.Config) {
	now := time.Now()

	// Clean up expired temporary unblocks
	CleanupExpiredUnblocks(now)

	// Check what changed
	hostsNeedsUpdate := false
	sudoersNeedsUpdate := false
	reason := ""

	enforcementState.mu.RLock()
	lastTimeWindowState := enforcementState.lastTimeWindowState
	lastTempUnblockCount := enforcementState.lastTempUnblockCount
	lastSudoersLocked := enforcementState.lastSudoersLocked
	expectedHostsHash := enforcementState.expectedHostsHash
	enforcementState.mu.RUnlock()

	// 1. Check if temp unblocks changed
	currentTempUnblocks := len(state.GetTempUnblocks())
	if currentTempUnblocks != lastTempUnblockCount {
		hostsNeedsUpdate = true
		reason = "temp unblocks changed"
	}

	// 2. Check if time window state changed for any domain
	if !hostsNeedsUpdate {
		currentTimeWindowState := buildTimeWindowState(cfg, now)
		for domain, wasBlocked := range lastTimeWindowState {
			if currentTimeWindowState[domain] != wasBlocked {
				hostsNeedsUpdate = true
				reason = "time window state changed for " + domain
				break
			}
		}
		// Also check for new domains in current state
		if !hostsNeedsUpdate {
			for domain := range currentTimeWindowState {
				if _, exists := lastTimeWindowState[domain]; !exists {
					hostsNeedsUpdate = true
					reason = "new domain in time window state"
					break
				}
			}
		}
	}

	// 3. Check if hosts file was tampered with
	if !hostsNeedsUpdate && cfg.EnableHosts && expectedHostsHash != "" {
		currentHash, err := computeFileChecksum(cfg.HostsPath)
		if err != nil {
			log.Printf("Warning: couldn't compute hosts checksum: %v", err)
		} else if currentHash != expectedHostsHash {
			hostsNeedsUpdate = true
			reason = "hosts file tampered"
			log.Printf("TAMPER DETECTED: hosts file checksum mismatch")
		}
	}

	// 4. Check if sudoers lock state changed
	if cfg.Sudoers.Enabled {
		currentSudoersLocked := !isSudoersAllowed(cfg, now)
		if currentSudoersLocked != lastSudoersLocked {
			sudoersNeedsUpdate = true
		}
	}

	// Apply updates if needed
	if hostsNeedsUpdate {
		log.Printf("Hosts update needed: %s", reason)
		blockedDomains := GetDomainsToBlock(cfg, now)

		if cfg.EnableHosts {
			if err := UpdateHosts(cfg, blockedDomains, false); err != nil {
				log.Printf("ERROR updating hosts: %v", err)
			} else {
				// Update stored hash
				if hash, err := computeFileChecksum(cfg.HostsPath); err == nil {
					enforcementState.mu.Lock()
					enforcementState.expectedHostsHash = hash
					enforcementState.lastBlockedCount = len(blockedDomains)
					enforcementState.mu.Unlock()
				}
			}
		}

		if cfg.EnableFirewall {
			if err := UpdateFirewall(blockedDomains, false); err != nil {
				log.Printf("ERROR updating firewall: %v", err)
			}
		}
	}

	if sudoersNeedsUpdate {
		log.Printf("Sudoers update needed: lock state changed")
		if err := UpdateSudoers(cfg, now, false, false); err != nil {
			log.Printf("ERROR updating sudoers: %v", err)
		}
	}

	// Self-heal check (lightweight - just re-applies immutable flags)
	if cfg.SelfHeal {
		SelfHeal(cfg)
	}

	// Update state
	enforcementState.mu.Lock()
	enforcementState.lastTimeWindowState = buildTimeWindowState(cfg, now)
	enforcementState.lastTempUnblockCount = currentTempUnblocks
	if cfg.Sudoers.Enabled {
		enforcementState.lastSudoersLocked = !isSudoersAllowed(cfg, now)
	}
	enforcementState.lastEnforcement = now
	enforcementState.mu.Unlock()
}

// ForceEnforcement forces a full enforcement cycle, typically called after config reload.
func ForceEnforcement(cfg *config.Config) {
	log.Println("Forcing full enforcement cycle...")
	InitialEnforcement(cfg)
}

// buildTimeWindowState creates a map of domain -> isBlockedByTimeWindow for current time.
func buildTimeWindowState(cfg *config.Config, now time.Time) map[string]bool {
	result := make(map[string]bool)
	currentDay := now.Weekday().String()[:3]
	currentTime := now.Format("15:04")

	for _, domain := range cfg.Domains {
		if domain.AlwaysBlock {
			// Always blocked domains don't change based on time
			continue
		}

		blocked := false
		for _, window := range domain.TimeWindows {
			// For midnight-crossing windows, check previous day for early morning times
			dayToCheck := currentDay
			if window.Start > window.End && currentTime <= window.End {
				dayToCheck = now.AddDate(0, 0, -1).Weekday().String()[:3]
			}

			if !containsDay(window.Days, dayToCheck) {
				continue
			}

			if isInTimeWindow(currentTime, window.Start, window.End) {
				blocked = true
				break
			}
		}
		result[domain.Name] = blocked
	}

	return result
}

// isSudoersAllowed checks if sudoers should be in "allowed" state based on time windows.
func isSudoersAllowed(cfg *config.Config, now time.Time) bool {
	currentDay := now.Weekday().String()[:3]
	currentTime := now.Format("15:04")

	for _, window := range cfg.Sudoers.TimeAllowed {
		if containsDay(window.Days, currentDay) && isInTimeWindow(currentTime, window.Start, window.End) {
			return true
		}
	}
	return false
}

// containsDay checks if a day is in the list of days.
func containsDay(days []string, day string) bool {
	return slices.Contains(days, day)
}

// isInTimeWindow checks if current time is within a time window.
func isInTimeWindow(currentTime, start, end string) bool {
	if start <= end {
		// Normal window (e.g., 09:00 to 17:00)
		return currentTime >= start && currentTime <= end
	}
	// Midnight-crossing window (e.g., 22:00 to 05:00)
	return currentTime >= start || currentTime <= end
}

// computeFileChecksum computes SHA256 checksum of a file.
func computeFileChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// GetEnforcementState returns a copy of the current enforcement state for status reporting.
func GetEnforcementState() (lastEnforcement time.Time, blockedCount int, hostsHash string) {
	enforcementState.mu.RLock()
	defer enforcementState.mu.RUnlock()
	return enforcementState.lastEnforcement, enforcementState.lastBlockedCount, enforcementState.expectedHostsHash
}
