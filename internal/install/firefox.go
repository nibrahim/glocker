package install

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"glocker/internal/utils"
)

// CreateFirefoxExtension creates the Firefox extension XPI file.
// It first checks for a signed extension in web-ext-artifacts, and if not found,
// creates an unsigned XPI from the extension source files.
func CreateFirefoxExtension() error {
	xpiPath := "/usr/local/share/glocker/glocker.xpi"

	// First, check if there's a signed extension in web-ext-artifacts
	artifactsDir := "extensions/firefox/web-ext-artifacts"
	if entries, err := os.ReadDir(artifactsDir); err == nil {
		// Look for XPI files in the artifacts directory
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".xpi") {
				signedXpiPath := filepath.Join(artifactsDir, entry.Name())
				log.Printf("Found signed extension: %s", signedXpiPath)

				// Create destination directory
				if err := os.MkdirAll(filepath.Dir(xpiPath), 0755); err != nil {
					return fmt.Errorf("failed to create XPI directory: %w", err)
				}

				// Copy the signed XPI to the installation location
				if err := utils.CopyFile(signedXpiPath, xpiPath); err != nil {
					return fmt.Errorf("failed to copy signed XPI: %w", err)
				}

				log.Printf("✓ Signed Firefox extension installed from %s to %s", signedXpiPath, xpiPath)
				return nil
			}
		}
	}

	// No signed extension found, create unsigned one from source
	log.Println("No signed extension found in web-ext-artifacts, creating unsigned XPI from source...")

	// Create temporary directory for building the XPI
	tempDir, err := os.MkdirTemp("", "glocker-firefox-build")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Copy extension files to temp directory
	extensionDir := "extensions/firefox"
	if err := utils.CopyDir(extensionDir, tempDir); err != nil {
		return fmt.Errorf("failed to copy extension files: %w", err)
	}

	// Create XPI file using zip
	if err := os.MkdirAll(filepath.Dir(xpiPath), 0755); err != nil {
		return fmt.Errorf("failed to create XPI directory: %w", err)
	}

	// Change to temp directory and create zip
	cmd := exec.Command("zip", "-r", xpiPath, ".")
	cmd.Dir = tempDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create XPI file: %w", err)
	}

	log.Printf("✓ Unsigned Firefox extension XPI created at %s", xpiPath)
	log.Println("  Note: For production use, sign the extension with 'web-ext sign' and place in extensions/firefox/web-ext-artifacts/")
	return nil
}

// InstallFirefoxExtension installs the Firefox extension via Firefox policies.
// This forces the extension to be installed for all Firefox users on the system.
func InstallFirefoxExtension() error {
	log.Println("Installing Firefox extension via policies...")

	// Create Firefox policies directory
	policiesDir := "/etc/firefox/policies"
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		return fmt.Errorf("failed to create policies directory: %w", err)
	}

	// Create policies.json content
	policiesContent := `{
  "policies": {
    "ExtensionSettings": {
      "glocker@nibrahim.net.in": {
        "installation_mode": "force_installed",
        "install_url": "file:///usr/local/share/glocker/glocker.xpi",
        "allowed_in_private_browsing": true
      }
    },
    "ExtensionUpdate": false,
    "DisablePrivateBrowsing": true
  }
}`

	// Write policies.json file
	policiesPath := filepath.Join(policiesDir, "policies.json")
	if err := os.WriteFile(policiesPath, []byte(policiesContent), 0644); err != nil {
		return fmt.Errorf("failed to write policies.json: %w", err)
	}

	log.Printf("✓ Firefox policies installed at %s", policiesPath)
	log.Println("✓ Firefox extension will be automatically installed on next Firefox launch")
	return nil
}

// UninstallFirefoxExtension removes the Firefox extension policies and XPI file.
func UninstallFirefoxExtension() error {
	// Remove Firefox policies file
	policiesPath := "/etc/firefox/policies/policies.json"
	if err := os.Remove(policiesPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove policies.json: %w", err)
	}

	// Remove policies directory if empty
	policiesDir := "/etc/firefox/policies"
	if err := os.Remove(policiesDir); err != nil && !os.IsNotExist(err) {
		// Directory might not be empty, that's okay
	}

	// Remove XPI file
	xpiPath := "/usr/local/share/glocker/glocker.xpi"
	if err := os.Remove(xpiPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove XPI file: %w", err)
	}

	// Remove glocker directory if empty
	glockerDir := "/usr/local/share/glocker"
	if err := os.Remove(glockerDir); err != nil && !os.IsNotExist(err) {
		// Directory might not be empty, that's okay
	}

	return nil
}
