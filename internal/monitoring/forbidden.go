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
			if isProgramForbidden(program, now, currentDay, currentTime) {
				killMatchingProcesses(cfg, program.Name)
			}
		}
	}
}

// isProgramForbidden decides whether a program should be killed right now.
// Precedence: a matching kill window always wins. If only allow windows are
// set, the program is forbidden outside them. If both lists are empty, the
// program is always forbidden (legacy default).
func isProgramForbidden(program config.ForbiddenProgram, now time.Time, currentDay, currentTime string) bool {
	if inAnyWindow(program.KillWindows, now, currentDay, currentTime) {
		slog.Debug("Program is forbidden in current kill window", "program", program.Name)
		return true
	}

	if len(program.AllowWindows) > 0 {
		if inAnyWindow(program.AllowWindows, now, currentDay, currentTime) {
			return false
		}
		slog.Debug("Program is forbidden outside its allow windows", "program", program.Name)
		return true
	}

	if len(program.KillWindows) == 0 {
		slog.Debug("Program has no windows - blocking completely", "program", program.Name)
		return true
	}

	return false
}

// inAnyWindow reports whether the current time falls inside any of the given
// windows, honoring day-of-week membership and overnight wraparound.
func inAnyWindow(windows []config.TimeWindow, now time.Time, currentDay, currentTime string) bool {
	for _, window := range windows {
		dayToCheck := currentDay
		if window.Start > window.End && currentTime <= window.End {
			// We're in the early-morning portion of a window that started yesterday.
			dayToCheck = now.AddDate(0, 0, -1).Weekday().String()[:3]
		}

		if !slices.Contains(window.Days, dayToCheck) {
			continue
		}

		if utils.IsInTimeWindow(currentTime, window.Start, window.End) {
			return true
		}
	}
	return false
}

// killMatchingProcesses finds and kills processes matching the given program name.
func killMatchingProcesses(cfg *config.Config, programName string) {
	// Use `comm` (kernel task name) for matching — it's the binary basename,
	// never contains spaces, so paths like "/home/noufal/GOG Games/.../FTL.amd64"
	// don't trip up whitespace-based field parsing the way `ps aux` does.
	cmd := exec.Command("ps", "-eo", "pid=,comm=,args=")
	output, err := cmd.Output()
	if err != nil {
		slog.Debug("Failed to get process list", "error", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	killedProcesses := []string{}
	processGroups := make(map[string][]state.ProcessInfo)

	slog.Debug("Starting process matching", "program_filter", programName, "total_lines", len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid := fields[0]
		comm := fields[1]

		if !strings.Contains(strings.ToLower(comm), strings.ToLower(programName)) {
			continue
		}

		slog.Debug("Extracted process info", "pid", pid, "comm", comm)

		// Don't kill our own process or system processes
		if strings.Contains(strings.ToLower(comm), "glocker") ||
			strings.Contains(strings.ToLower(comm), "systemd") ||
			strings.Contains(strings.ToLower(comm), "kernel") ||
			pid == "1" {
			slog.Debug("Skipping protected process", "pid", pid, "comm", comm)
			continue
		}

		processInfo := state.ProcessInfo{
			PID:         pid,
			Name:        comm,
			CommandLine: line,
		}

		processGroups[pid] = append(processGroups[pid], processInfo)
	}

	// Kill matching processes
	violationRecorded := false // Track if we've recorded a violation for this program
	for _, processes := range processGroups {
		if len(processes) == 0 {
			continue
		}

		proc := processes[0]
		slog.Debug("Found forbidden process", "pid", proc.PID, "name", proc.Name)

		// Record violation only once per program name (not per subprocess)
		if !violationRecorded && cfg.ViolationTracking.Enabled {
			RecordViolation(cfg, "forbidden_program", programName, fmt.Sprintf("Killed %d process(es)", len(processGroups)))
			violationRecorded = true
		}

		// Kill the process
		if err := exec.Command("kill", proc.PID).Run(); err == nil {
			killedProcesses = append(killedProcesses, fmt.Sprintf("%s (PID: %s)", proc.Name, proc.PID))
			log.Printf("KILLED FORBIDDEN PROGRAM: %s (PID: %s) - matched filter: %s", proc.Name, proc.PID, programName)

			// Wait then force kill if still running
			time.Sleep(2 * time.Second)
			exec.Command("kill", "-9", proc.PID).Run()
		}
	}

	// Send desktop notification once for all killed processes
	if len(killedProcesses) > 0 {
		message := fmt.Sprintf("Terminated forbidden program: %s (%d process(es))", programName, len(killedProcesses))
		notify.SendNotification(cfg, "Glocker Alert", message, "normal", "dialog-warning")
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

