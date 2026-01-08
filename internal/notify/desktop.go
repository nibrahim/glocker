package notify

import (
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"glocker/internal/config"
)

// SendNotification sends a desktop notification using the configured command.
// Placeholder variables {title}, {message}, {urgency}, and {icon} in the
// NotificationCommand are replaced with the provided values.
// Returns silently if NotificationCommand is not configured.
func SendNotification(cfg *config.Config, title, message, urgency, icon string) {
	if cfg.NotificationCommand == "" {
		return
	}

	// Replace placeholders in the command
	cmd := cfg.NotificationCommand
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
