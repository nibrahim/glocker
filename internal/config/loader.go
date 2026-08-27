package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadConfig reads and parses the glocker configuration from the installed
// config file, resolving any !include directives. Returns an error if the file
// doesn't exist or cannot be parsed.
func LoadConfig() (*Config, error) {
	if _, err := os.Stat(GlockerConfigFile); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found at %s\n\nThis usually means glocker is not properly installed.\nPlease check:\n  1. Is glocker installed? Run: ls -la %s\n  2. Is the glocker service running? Run: systemctl status glocker.service\n  3. If not installed, run: sudo glocker -install\n\nOriginal error: %w", GlockerConfigFile, InstallPath, err)
		}
		return nil, fmt.Errorf("config file access error at %s: %w", GlockerConfigFile, err)
	}

	slog.Debug("Loading config from external file", "path", GlockerConfigFile)
	return LoadFile(GlockerConfigFile)
}

// LoadFile reads and parses a glocker config file, resolving !include directives
// relative to the file's own directory. Use this instead of a raw
// yaml.Unmarshal so includes work from every caller (daemon, installer,
// configcheck).
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return ParseConfig(data, filepath.Dir(path))
}

// ParseConfig parses config bytes, resolving !include directives against
// baseDir, and decodes the result into a Config.
func ParseConfig(data []byte, baseDir string) (*Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	if err := resolveIncludes(&root, baseDir, 0); err != nil {
		return nil, err
	}
	// An empty file decodes to a zero Config, matching the old behaviour.
	if root.Kind == 0 {
		return &Config{}, nil
	}
	var cfg Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decoding config: %w", err)
	}
	return &cfg, nil
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
