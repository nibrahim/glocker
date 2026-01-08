package cli

import (
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"glocker/internal/config"
	"glocker/internal/enforcement"
	"glocker/internal/state"
)

// GetStatusResponse returns a formatted status report for the system.
func GetStatusResponse(cfg *config.Config) string {
	var response strings.Builder
	now := time.Now()

	response.WriteString("╔════════════════════════════════════════════════╗\n")
	response.WriteString("║                 LIVE STATUS                    ║\n")
	response.WriteString("╚════════════════════════════════════════════════╝\n\n")

	// Current time and service status
	response.WriteString(fmt.Sprintf("Current Time: %s\n", now.Format("2006-01-02 15:04:05")))
	response.WriteString(fmt.Sprintf("Service Status: Running\n"))
	response.WriteString(fmt.Sprintf("Enforcement Interval: %d seconds\n\n", cfg.EnforceInterval))

	// Get currently blocked domains
	blockedDomains := enforcement.GetDomainsToBlock(cfg, now)
	response.WriteString(fmt.Sprintf("Currently Blocked Domains: %d\n", len(blockedDomains)))

	// Show temporary unblocks
	unblocks := state.GetTempUnblocks()
	activeUnblocks := 0
	for _, unblock := range unblocks {
		if now.Before(unblock.ExpiresAt) {
			activeUnblocks++
		}
	}
	response.WriteString(fmt.Sprintf("Temporary Unblocks: %d active\n", activeUnblocks))

	if activeUnblocks > 0 {
		response.WriteString("  Active temporary unblocks:\n")
		for _, unblock := range unblocks {
			if now.Before(unblock.ExpiresAt) {
				remaining := unblock.ExpiresAt.Sub(now)
				response.WriteString(fmt.Sprintf("    - %s (expires in %v)\n", unblock.Domain, remaining.Round(time.Minute)))
			}
		}
	}
	response.WriteString("\n")

	// Count domains by type
	alwaysBlockCount := 0
	timeBasedCount := 0
	loggedCount := 0
	for _, domain := range cfg.Domains {
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
		len(cfg.Domains), alwaysBlockCount, timeBasedCount, loggedCount))

	// Show violation tracking status
	if cfg.ViolationTracking.Enabled {
		violations := state.GetViolations()
		recentViolations := 0
		cutoff := now.Add(-time.Duration(cfg.ViolationTracking.TimeWindowMinutes) * time.Minute)
		for _, v := range violations {
			if v.Timestamp.After(cutoff) {
				recentViolations++
			}
		}

		response.WriteString("\n")
		response.WriteString("Violation Tracking:\n")
		response.WriteString(fmt.Sprintf("  Recent Violations: %d/%d (in last %d minutes)\n",
			recentViolations, cfg.ViolationTracking.MaxViolations, cfg.ViolationTracking.TimeWindowMinutes))
		response.WriteString(fmt.Sprintf("  Total Violations: %d\n", len(violations)))
	}

	// Show time-based blocked domains
	if timeBasedCount > 0 {
		response.WriteString("\n")
		response.WriteString(fmt.Sprintf("Time-Based Domains (%d):\n", timeBasedCount))
		count := 0
		for _, domain := range cfg.Domains {
			if !domain.AlwaysBlock && len(domain.TimeWindows) > 0 {
				response.WriteString(fmt.Sprintf("  %s: %s\n", domain.Name, formatTimeWindows(domain.TimeWindows)))
				count++
				if count >= 10 {
					response.WriteString(fmt.Sprintf("  ... and %d more\n", timeBasedCount-10))
					break
				}
			}
		}
	}

	// Show forbidden programs with time windows
	if cfg.EnableForbiddenPrograms && cfg.ForbiddenPrograms.Enabled && len(cfg.ForbiddenPrograms.Programs) > 0 {
		response.WriteString("\n")
		response.WriteString(fmt.Sprintf("Forbidden Programs (%d):\n", len(cfg.ForbiddenPrograms.Programs)))
		for _, program := range cfg.ForbiddenPrograms.Programs {
			if len(program.TimeWindows) > 0 {
				response.WriteString(fmt.Sprintf("  %s: %s\n", program.Name, formatTimeWindows(program.TimeWindows)))
			} else {
				response.WriteString(fmt.Sprintf("  %s: always\n", program.Name))
			}
		}
	}

	// Show panic mode status
	panicUntil := state.GetPanicUntil()
	if !panicUntil.IsZero() && now.Before(panicUntil) {
		remaining := panicUntil.Sub(now)
		response.WriteString("\n")
		response.WriteString("⚠️  PANIC MODE ACTIVE ⚠️\n")
		response.WriteString(fmt.Sprintf("Time Remaining: %v\n", remaining.Round(time.Second)))
	}

	response.WriteString("\nEND\n")
	return response.String()
}

// formatTimeWindows converts time windows to a readable string.
func formatTimeWindows(windows []config.TimeWindow) string {
	if len(windows) == 0 {
		return "always"
	}

	var parts []string
	for _, window := range windows {
		days := strings.Join(window.Days, ",")
		parts = append(parts, fmt.Sprintf("%s-%s (%s)", window.Start, window.End, days))
	}
	return strings.Join(parts, "; ")
}

// ProcessReloadRequest reloads the configuration.
func ProcessReloadRequest(cfg *config.Config) {
	slog.Debug("Processing reload request")

	newCfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("ERROR: Failed to reload config: %v", err)
		return
	}

	// Validate new config
	if err := config.ValidateConfig(newCfg); err != nil {
		log.Printf("ERROR: Invalid config: %v", err)
		return
	}

	// Replace config pointer contents
	*cfg = *newCfg

	// Re-apply enforcement with new config
	enforcement.RunOnce(cfg, false)

	log.Println("✓ Configuration reloaded successfully")
}

// ProcessUnblockRequest processes a temporary unblock request.
func ProcessUnblockRequest(cfg *config.Config, hostsStr, reason string) {
	slog.Debug("Processing unblock request", "hosts", hostsStr, "reason", reason)

	hosts := strings.Split(hostsStr, ",")
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		// Add to temporary unblocks
		duration := time.Duration(cfg.Unblocking.TempUnblockTime) * time.Minute
		if duration == 0 {
			duration = 30 * time.Minute
		}
		expiresAt := time.Now().Add(duration)

		state.AddTempUnblock(host, expiresAt)

		log.Printf("UNBLOCKED: %s (reason: %s) until %s", host, reason, expiresAt.Format("15:04:05"))
	}

	// Re-apply enforcement immediately
	enforcement.RunOnce(cfg, false)
}

// ProcessBlockRequest adds domains to the block list.
func ProcessBlockRequest(cfg *config.Config, hostsStr string) {
	slog.Debug("Processing block request", "hosts", hostsStr)

	hosts := strings.Split(hostsStr, ",")
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		// Add to config domains as always-block
		cfg.Domains = append(cfg.Domains, config.Domain{
			Name:        host,
			AlwaysBlock: true,
		})

		log.Printf("BLOCKED: %s", host)
	}

	// Re-apply enforcement immediately
	enforcement.RunOnce(cfg, false)
}

// ProcessPanicRequest activates panic mode for the specified duration.
func ProcessPanicRequest(cfg *config.Config, minutes int) {
	slog.Debug("Processing panic request", "minutes", minutes)

	now := time.Now()
	panicUntil := now.Add(time.Duration(minutes) * time.Minute)
	state.SetPanicUntil(panicUntil)

	log.Printf("⚠️  PANIC MODE ACTIVATED for %d minutes (until %s)", minutes, panicUntil.Format("15:04:05"))

	// Immediately suspend the system if panic command is configured
	if cfg.PanicCommand != "" {
		// The monitoring goroutine will handle suspension
	}
}
