package monitoring

import (
	"fmt"
	"log"
	"log/slog"
	"os/exec"
	"slices"
	"strings"
	"time"

	"glocker/internal/config"
	"glocker/internal/state"
	"glocker/internal/utils"
	"glocker/internal/notify"
)

// MonitorForbiddenPrograms continuously monitors and kills forbidden programs based on time windows.
func MonitorForbiddenPrograms(cfg *config.Config) {
	// Set default check interval if not specified
	checkInterval := cfg.ForbiddenPrograms.CheckInterval
	if checkInterval == 0 {
		checkInterval = 5 // Default: check every 5 seconds
	}

	slog.Debug("Starting forbidden programs monitoring", "check_interval_seconds", checkInterval, "programs_count", len(cfg.ForbiddenPrograms.Programs))

	ticker := time.NewTicker(time.Duration(checkInterval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		currentDay := now.Weekday().String()[:3]
		currentTime := now.Format("15:04")

		slog.Debug("Checking for forbidden programs", "current_day", currentDay, "current_time", currentTime)

		for _, program := range cfg.ForbiddenPrograms.Programs {
			// Check if any time window is active for this program
			programForbidden := false

			// If no time windows specified, block the program completely (always forbidden)
			if len(program.TimeWindows) == 0 {
				programForbidden = true
				slog.Debug("Program has no time windows - blocking completely", "program", program.Name)
			} else {
				// Check time windows
				for _, window := range program.TimeWindows {
					dayToCheck := currentDay
					if window.Start > window.End && currentTime <= window.End {
						// We're in the early morning portion of a window that started yesterday
						yesterday := now.AddDate(0, 0, -1).Weekday().String()[:3]
						dayToCheck = yesterday
						slog.Debug("Checking previous day for wraparound window", "current_day", currentDay, "checking_day", dayToCheck)
					}

					if !slices.Contains(window.Days, dayToCheck) {
						continue
					}

					if utils.IsInTimeWindow(currentTime, window.Start, window.End) {
						programForbidden = true
						slog.Debug("Program is forbidden in current time window", "program", program.Name, "window", fmt.Sprintf("%s-%s", window.Start, window.End))
						break
					}
				}
			}

			if programForbidden {
				killMatchingProcesses(cfg, program.Name)
			}
		}
	}
}

// killMatchingProcesses finds and kills processes matching the given program name.
func killMatchingProcesses(cfg *config.Config, programName string) {
	// Get list of running processes
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Failed to get process list", "error", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	killedProcesses := []string{}
	processGroups := make(map[string][]state.ProcessInfo)

	slog.Debug("Starting process matching", "program_filter", programName, "total_lines", len(lines))

	// Collect all matching processes
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

			slog.Debug("Extracted process info", "pid", pid, "extracted_name", processName)

			// Don't kill our own process or system processes
			if strings.Contains(strings.ToLower(processName), "glocker") ||
				strings.Contains(strings.ToLower(processName), "systemd") ||
				strings.Contains(strings.ToLower(processName), "kernel") ||
				pid == "1" {
				slog.Debug("Skipping protected process", "pid", pid, "name", processName)
				continue
			}

			processInfo := state.ProcessInfo{
				PID:         pid,
				Name:        processName,
				CommandLine: line,
			}

			processGroups[pid] = append(processGroups[pid], processInfo)
		}
	}

	// Kill matching processes
	for _, processes := range processGroups {
		if len(processes) == 0 {
			continue
		}

		proc := processes[0]
		slog.Debug("Found forbidden process", "pid", proc.PID, "name", proc.Name)

		// Record violation
		if cfg.ViolationTracking.Enabled {
			RecordViolation(cfg, "forbidden_program", proc.Name, fmt.Sprintf("PID: %s", proc.PID))
		}

		// Kill the process
		if err := exec.Command("kill", proc.PID).Run(); err == nil {
			killedProcesses = append(killedProcesses, fmt.Sprintf("%s (PID: %s)", proc.Name, proc.PID))
			log.Printf("KILLED FORBIDDEN PROGRAM: %s (PID: %s) - matched filter: %s", proc.Name, proc.PID, programName)

			// Send desktop notification
			notify.SendNotification(cfg, "Glocker Alert",
				fmt.Sprintf("Terminated forbidden program: %s", proc.Name),
				"normal", "dialog-warning")

			// Wait then force kill if still running
			time.Sleep(2 * time.Second)
			exec.Command("kill", "-9", proc.PID).Run()
		}
	}

	// Send accountability email if processes were killed
	if len(killedProcesses) > 0 && cfg.Accountability.Enabled {
		subject := "GLOCKER ALERT: Forbidden Programs Terminated"
		body := fmt.Sprintf("Forbidden programs were detected and terminated at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
		body += fmt.Sprintf("Filter: %s\n", programName)
		body += "Terminated processes:\n"
		for _, proc := range killedProcesses {
			body += fmt.Sprintf("  - %s\n", proc)
		}

		notify.SendEmail(cfg, subject, body)
	}
}

// extractProcessName extracts the process name from a ps aux output line.
func extractProcessName(psLine string) string {
	fields := strings.Fields(psLine)
	if len(fields) >= 11 {
		// Return the command (11th field in ps aux)
		return fields[10]
	}
	return "unknown"
}
