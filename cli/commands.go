package cli

import (
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"glocker/config"
	"glocker/enforcement"
	"glocker/internal/state"
)

// GetStatusResponse returns a formatted status report for the system.
func GetStatusResponse(cfg *config.Config) string {
	var response strings.Builder
	now := time.Now()

	response.WriteString("╔════════════════════════════════════════════════╗\n")
	response.WriteString("║                 LIVE STATUS                    ║\n")
	response.WriteString("╚════════════════════════════════════════════════╝\n\n")

	// Current time and blocking state
	response.WriteString(fmt.Sprintf("Current Time: %s\n", now.Format("2006-01-02 15:04:05")))
	currentDay := now.Weekday().String()[:3]
	currentTime := now.Format("15:04")
	response.WriteString(fmt.Sprintf("Day/Time: %s %s\n\n", currentDay, currentTime))

	// Domains currently being blocked
	blockedDomains := enforcement.GetDomainsToBlock(cfg, now)
	response.WriteString(fmt.Sprintf("Currently Blocking: %d domains\n", len(blockedDomains)))

	// Temporary unblocks
	unblocks := state.GetTempUnblocks()
	response.WriteString(fmt.Sprintf("Temporary Unblocks: %d active\n", len(unblocks)))

	// Panic mode status
	panicUntil := state.GetPanicUntil()
	if !panicUntil.IsZero() && now.Before(panicUntil) {
		remaining := panicUntil.Sub(now)
		response.WriteString(fmt.Sprintf("\n⚠️  PANIC MODE ACTIVE ⚠️\n"))
		response.WriteString(fmt.Sprintf("Time Remaining: %v\n", remaining.Round(time.Second)))
	}

	response.WriteString("\nEND\n")
	return response.String()
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
