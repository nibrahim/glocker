package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadConfig reads and parses the glocker configuration from the config file.
// Returns an error if the file doesn't exist or cannot be parsed.
func LoadConfig() (*Config, error) {
	var config Config

	// Read from external config file
	if _, err := os.Stat(GlockerConfigFile); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found at %s\n\nThis usually means glocker is not properly installed.\nPlease check:\n  1. Is glocker installed? Run: ls -la %s\n  2. Is the glocker service running? Run: systemctl status glocker.service\n  3. If not installed, run: sudo glocker -install\n\nOriginal error: %w", GlockerConfigFile, InstallPath, err)
		}
		return nil, fmt.Errorf("config file access error at %s: %w", GlockerConfigFile, err)
	}

	slog.Debug("Loading config from external file", "path", GlockerConfigFile)
	configData, err := os.ReadFile(GlockerConfigFile)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	if err := yaml.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &config, nil
}

// SetupLogging initializes the structured logging system based on the config.
// Sets the log level from config and configures the default slog logger.
func SetupLogging(cfg *Config) {
	var level slog.Level

	switch strings.ToLower(cfg.LogLevel) {
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
