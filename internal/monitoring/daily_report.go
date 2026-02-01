package monitoring

import (
	"fmt"
	"log"
	"strings"
	"time"

	"glocker/internal/config"
	"glocker/internal/notify"
	"glocker/internal/reports"
	"glocker/internal/state"
)

// MonitorDailyReport runs a background goroutine that sends daily reports at the configured time.
// Reports contain the previous day's data (violations, unblocks, unmanaged periods).
func MonitorDailyReport(cfg *config.Config) {
	if !cfg.Accountability.DailyReportEnabled {
		return
	}

	reportTime := cfg.Accountability.DailyReportTime
	if reportTime == "" {
		reportTime = "08:00" // Default to 8am
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		currentTime := now.Format("15:04")

		// Check if it's time to send the report
		if currentTime == reportTime {
			// Check if we've already sent today's report
			lastSent, exists := state.GetLastEmailTime("daily_report")
			if exists && lastSent.Day() == now.Day() && lastSent.Month() == now.Month() && lastSent.Year() == now.Year() {
				continue // Already sent today
			}

			// Send report for yesterday
			yesterday := now.AddDate(0, 0, -1)
			if err := sendDailyReport(cfg, yesterday); err != nil {
				log.Printf("Failed to send daily report: %v", err)
			} else {
				state.SetLastEmailTime("daily_report", now)
				log.Printf("Daily report sent for %s", yesterday.Format("2006-01-02"))
			}
		}
	}
}

// sendDailyReport generates and sends the daily report email for a specific date.
func sendDailyReport(cfg *config.Config, date time.Time) error {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24*time.Hour - time.Second)

	// Gather violations
	violations, _ := reports.ParseReportsLog("")
	violations = reports.FilterReports(violations, reports.ReportFilter{
		StartTime: &dayStart,
		EndTime:   &dayEnd,
	})

	// Gather unblocks
	unblocks, _ := reports.ParseUnblocksLog("")
	unblocks = reports.FilterUnblocks(unblocks, reports.UnblockFilter{
		StartTime: &dayStart,
		EndTime:   &dayEnd,
	})

	// Gather lifecycle events (unmanaged periods)
	lifecycle, _ := reports.ParseLifecycleLog("")
	lifecycle = reports.FilterLifecycle(lifecycle, reports.LifecycleFilter{
		StartTime: &dayStart,
		EndTime:   &dayEnd,
	})

	// Calculate unmanaged time
	unmanagedMinutes := calculateUnmanagedMinutes(date)

	// Build report body
	var body strings.Builder
	body.WriteString(fmt.Sprintf("Daily Glocker Report for %s\n", date.Format("Monday, January 2, 2006")))
	body.WriteString("=" + strings.Repeat("=", 50) + "\n\n")

	// Summary section
	body.WriteString("SUMMARY\n")
	body.WriteString(strings.Repeat("-", 30) + "\n")
	body.WriteString(fmt.Sprintf("Violations:     %d\n", len(violations)))
	body.WriteString(fmt.Sprintf("Unblocks:       %d\n", len(unblocks)))
	if unmanagedMinutes > 0 {
		body.WriteString(fmt.Sprintf("Unmanaged time: %d minutes\n", unmanagedMinutes))
	}
	body.WriteString("\n")

	// Violations details
	if len(violations) > 0 {
		body.WriteString("VIOLATIONS\n")
		body.WriteString(strings.Repeat("-", 30) + "\n")

		// Group by keyword
		keywords := make(map[string]int)
		for _, v := range violations {
			keywords[v.Keyword]++
		}
		for kw, count := range keywords {
			body.WriteString(fmt.Sprintf("  %s: %d\n", kw, count))
		}
		body.WriteString("\n")
	}

	// Unblocks details
	if len(unblocks) > 0 {
		body.WriteString("UNBLOCKS\n")
		body.WriteString(strings.Repeat("-", 30) + "\n")
		for _, u := range unblocks {
			duration := int(u.RestoreTime.Sub(u.UnblockTime).Minutes())
			body.WriteString(fmt.Sprintf("  %s - %s (%d min) - \"%s\"\n",
				u.UnblockTime.Format("15:04"),
				u.Domain,
				duration,
				u.Reason))
		}
		body.WriteString("\n")
	}

	// Lifecycle events
	if len(lifecycle) > 0 {
		body.WriteString("LIFECYCLE EVENTS\n")
		body.WriteString(strings.Repeat("-", 30) + "\n")
		for _, e := range lifecycle {
			if e.Reason != "" {
				body.WriteString(fmt.Sprintf("  %s - %s (%s)\n",
					e.Timestamp.Format("15:04"),
					e.Type,
					e.Reason))
			} else {
				body.WriteString(fmt.Sprintf("  %s - %s\n",
					e.Timestamp.Format("15:04"),
					e.Type))
			}
		}
		body.WriteString("\n")
	}

	// Determine subject based on content
	subject := fmt.Sprintf("Glocker Daily Report: %s", date.Format("Jan 2"))
	if len(violations) > 10 || unmanagedMinutes > 30 {
		subject = fmt.Sprintf("Glocker Daily Report [ATTENTION]: %s", date.Format("Jan 2"))
	}

	return notify.SendEmail(cfg, subject, body.String())
}

// calculateUnmanagedMinutes calculates the total unmanaged minutes for a specific day.
func calculateUnmanagedMinutes(date time.Time) int {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	entries, err := reports.ParseLifecycleLog("")
	if err != nil {
		return 0
	}

	totalMinutes := 0
	var currentUninstall *time.Time

	for _, e := range entries {
		if e.Type == "uninstall" {
			currentUninstall = &e.Timestamp
		} else if e.Type == "install" && currentUninstall != nil {
			// Calculate overlap with the target day
			start := *currentUninstall
			end := e.Timestamp

			// Skip very short periods (upgrades)
			if end.Sub(start) < 2*time.Minute {
				currentUninstall = nil
				continue
			}

			// Clamp to day boundaries
			if start.Before(dayStart) {
				start = dayStart
			}
			if end.After(dayEnd) {
				end = dayEnd
			}

			// Only count if there's overlap
			if start.Before(end) {
				totalMinutes += int(end.Sub(start).Minutes())
			}

			currentUninstall = nil
		}
	}

	// Handle ongoing unmanaged period
	if currentUninstall != nil {
		start := *currentUninstall
		end := time.Now()
		if end.After(dayEnd) {
			end = dayEnd
		}
		if start.Before(dayStart) {
			start = dayStart
		}
		if start.Before(end) {
			totalMinutes += int(end.Sub(start).Minutes())
		}
	}

	return totalMinutes
}
