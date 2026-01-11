// Command glocklock is a screen locker utility for glocker.
//
// Usage:
//
//	# Time-based lock using config defaults
//	glocklock
//
//	# Time-based lock with custom duration
//	glocklock -duration 5m -message "Taking a break"
//
//	# Text-based lock using mindful_text from config
//	glocklock -mindful
//
//	# Text-based lock from file
//	glocklock -text /path/to/file.txt
//
//	# Use custom config file
//	glocklock -conf /path/to/config.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"glocker/internal/config"
	"glocker/internal/lock"
)

const defaultDuration = 1 * time.Minute

func main() {
	confPath := flag.String("conf", config.GlockerConfigFile, "Path to config file")
	duration := flag.Duration("duration", 0, "Lock duration (overrides config)")
	message := flag.String("message", "Screen locked", "Message to display")
	textFile := flag.String("text", "", "Path to text file (enables text-based lock)")
	mindful := flag.Bool("mindful", false, "Use mindful_text from config for text-based lock")
	flag.Parse()

	// Load config (errors are non-fatal, we just use defaults)
	cfg := loadConfig(*confPath)

	// Determine effective duration
	effectiveDuration := defaultDuration
	if cfg != nil && cfg.ViolationTracking.LockDuration != "" {
		if d, err := time.ParseDuration(cfg.ViolationTracking.LockDuration); err == nil {
			effectiveDuration = d
		}
	}
	if *duration != 0 {
		effectiveDuration = *duration // Command-line flag overrides config
	}

	// Text-based lock from mindful flag (uses config's mindful_text)
	if *mindful {
		if cfg == nil || cfg.ViolationTracking.MindfulText == "" {
			fmt.Fprintln(os.Stderr, "Error: -mindful requires mindful_text in config")
			os.Exit(1)
		}

		fmt.Println("Locking screen until mindful text is typed...")
		fmt.Println("Type the displayed text exactly and press Enter to unlock.")

		locker, err := lock.NewTextLocker(lock.TextLockConfig{
			TargetText: cfg.ViolationTracking.MindfulText,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating text locker: %v\n", err)
			os.Exit(1)
		}

		if err := locker.Lock(); err != nil {
			fmt.Fprintf(os.Stderr, "Error locking screen: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Screen unlocked.")
		return
	}

	// Text-based lock from file
	if *textFile != "" {
		fmt.Printf("Locking screen until text from %s is typed...\n", *textFile)
		fmt.Println("Type the displayed text exactly and press Enter to unlock.")

		locker, err := lock.NewTextLockerFromFile(*textFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating text locker: %v\n", err)
			os.Exit(1)
		}

		if err := locker.Lock(); err != nil {
			fmt.Fprintf(os.Stderr, "Error locking screen: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Screen unlocked.")
		return
	}

	// Time-based lock mode (default)
	fmt.Printf("Locking screen for %v...\n", effectiveDuration)
	fmt.Println("The screen will automatically unlock when the timer expires.")

	locker, err := lock.New(lock.Config{
		Duration: effectiveDuration,
		Message:  *message,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating locker: %v\n", err)
		os.Exit(1)
	}

	if err := locker.Lock(); err != nil {
		fmt.Fprintf(os.Stderr, "Error locking screen: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Screen unlocked.")
}

// loadConfig attempts to load the config file.
// Returns nil if the config cannot be loaded (file missing, invalid, etc.)
func loadConfig(path string) *config.Config {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	return &cfg
}
