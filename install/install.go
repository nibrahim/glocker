package install

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"glocker/config"
	"glocker/internal/utils"
)

const (
	SystemdServiceFile = "extras/glocker.service"
)

// InstallGlocker performs the complete installation of Glocker on the system.
// This includes copying the binary, config file, setting up systemd service,
// and installing the Firefox extension.
func InstallGlocker() error {
	log.Println("╔════════════════════════════════════════════════╗")
	log.Println("║              GLOCKER FULL INSTALL              ║")
	log.Println("╚════════════════════════════════════════════════╝")
	log.Println()

	// Step 1: Validate config file before installation
	log.Println("Validating configuration file...")
	configData, err := os.ReadFile("conf/conf.yaml")
	if err != nil {
		return fmt.Errorf("failed to read config file conf/conf.yaml: %w", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		return fmt.Errorf("invalid YAML in config file conf/conf.yaml: %w", err)
	}

	if err := config.ValidateConfig(&cfg); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	log.Println("✓ Configuration file is valid")

	// Step 2: Get current executable path
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	exePath, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Step 3: Copy config file from conf/conf.yaml to target location
	log.Printf("Copying config file from conf/conf.yaml to %s", config.GlockerConfigFile)

	// Create config directory if it doesn't exist
	configDir := filepath.Dir(config.GlockerConfigFile)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Copy the config file
	if err := utils.CopyFile("conf/conf.yaml", config.GlockerConfigFile); err != nil {
		return fmt.Errorf("failed to copy config file: %w", err)
	}
	log.Printf("✓ Config file copied to %s", config.GlockerConfigFile)

	// Set ownership and make config file immutable
	if err := os.Chown(config.GlockerConfigFile, 0, 0); err != nil {
		log.Printf("Warning: couldn't set config file ownership: %v", err)
	}
	if err := exec.Command("chattr", "+i", config.GlockerConfigFile).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable flag on config file: %v", err)
	}

	// Step 4: Copy binary to install location
	log.Printf("Installing binary to %s", config.InstallPath)
	if err := utils.CopyFile(exePath, config.InstallPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Set ownership to root:root
	if err := os.Chown(config.InstallPath, 0, 0); err != nil {
		log.Printf("Warning: couldn't set ownership to root: %v", err)
	}

	// Set setuid bit (4755 = rwsr-xr-x)
	if err := os.Chmod(config.InstallPath, 0o755|os.ModeSetuid|os.ModeSetgid); err != nil {
		return fmt.Errorf("failed to set setuid bit: %w", err)
	}

	// Set immutable on the installed binary
	if err := exec.Command("chattr", "+i", config.InstallPath).Run(); err != nil {
		log.Printf("Warning: couldn't set immutable flag: %v", err)
	}
	log.Println("✓ Binary installed with setuid permissions")

	// Step 5: Create and install Firefox extension
	if err := CreateFirefoxExtension(); err != nil {
		log.Printf("Warning: Failed to create Firefox extension: %v", err)
	} else if err := InstallFirefoxExtension(); err != nil {
		log.Printf("Warning: Failed to install Firefox extension: %v", err)
	}

	// Step 6: Install systemd service
	servicePath := "/etc/systemd/system/glocker.service"
	log.Println("Installing systemd service...")
	if err := utils.CopyFile(SystemdServiceFile, servicePath); err != nil {
		return fmt.Errorf("failed to create service file: %w", err)
	}

	// Reload systemd daemon
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	// Enable service
	if err := exec.Command("systemctl", "enable", "glocker.service").Run(); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}

	// Start service
	if err := exec.Command("systemctl", "start", "glocker.service").Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	// Protect service file
	exec.Command("chattr", "+i", servicePath).Run()

	log.Println("✓ Systemd service installed and started")
	log.Println()
	log.Println("🎉 Installation complete!")
	log.Println("   Run 'glocker -status' to check the current status")

	return nil
}

// RunningAsRoot checks if the process is running with root privileges.
// If real is true, checks the real user ID; otherwise checks effective user ID.
func RunningAsRoot(real bool) bool {
	var uid int
	if real {
		uid = os.Getuid() // Real user ID - who actually ran the command
	} else {
		uid = os.Geteuid() // Effective user ID - current privileges (affected by setuid)
	}
	return uid == 0
}
