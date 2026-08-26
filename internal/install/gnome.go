package install

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"glocker/internal/config"
	"glocker/internal/utils"
)

const gnomeExtUUID = "glocker-usage@glocker.app"

// InstallGnomeExtension installs + enables the GNOME Shell window-bridge
// extension for the desktop user, so glocker can track windows on GNOME/Wayland
// (Mutter otherwise blocks it). Best-effort and self-skipping: it does nothing
// on non-GNOME systems, on GNOME older than 45 (this is the ES-module build), or
// when no desktop user is known. On Wayland the user must restart their session
// (the shell can't hot-load a new extension) to activate it.
func InstallGnomeExtension(cfg *config.Config) {
	src := filepath.Join("extensions", "gnome", gnomeExtUUID)
	if _, err := os.Stat(src); err != nil {
		return // not installing from a tree that carries the extension
	}
	if _, err := exec.LookPath("gnome-shell"); err != nil {
		return // not a GNOME system
	}
	if v := gnomeShellMajor(); v > 0 && v < 45 {
		log.Printf("GNOME extension: shell %d is older than 45 (needs a legacy build); skipping", v)
		return
	}
	username := desktopUser(cfg)
	if username == "" {
		log.Println("GNOME extension: no desktop user known (set usage_monitor.user); skipping")
		return
	}
	u, err := user.Lookup(username)
	if err != nil {
		log.Printf("GNOME extension: user %q lookup failed (%v); skipping", username, err)
		return
	}

	dst := filepath.Join(u.HomeDir, ".local", "share", "gnome-shell", "extensions", gnomeExtUUID)
	if err := copyExtensionTo(src, dst, u); err != nil {
		log.Printf("GNOME extension: install failed (%v)", err)
		return
	}
	log.Printf("✓ GNOME window-tracking extension installed for %s", username)

	if err := enableExtensionForUser(username, u.Uid); err != nil {
		log.Printf("  couldn't auto-enable it (%v)", err)
		log.Printf("  enable it in your GNOME session: gnome-extensions enable %s", gnomeExtUUID)
	}
	log.Println("  On Wayland, log out and back in to activate window tracking.")
}

// desktopUser is the user whose GNOME session to target: explicit config first,
// then whoever ran sudo, then the sudoers-controlled user.
func desktopUser(cfg *config.Config) string {
	if cfg.UsageMonitor.User != "" {
		return cfg.UsageMonitor.User
	}
	if u := os.Getenv("SUDO_USER"); u != "" && u != "root" {
		return u
	}
	return cfg.Sudoers.User
}

// gnomeShellMajor returns the GNOME Shell major version, or 0 if unknown.
func gnomeShellMajor() int {
	out, err := exec.Command("gnome-shell", "--version").Output()
	if err != nil {
		return 0
	}
	return parseGnomeMajor(string(out))
}

// parseGnomeMajor pulls the major version out of e.g. "GNOME Shell 48.7".
func parseGnomeMajor(s string) int {
	for _, f := range strings.Fields(s) {
		if n, err := strconv.Atoi(strings.SplitN(f, ".", 2)[0]); err == nil {
			return n
		}
	}
	return 0
}

func copyExtensionTo(src, dst string, u *user.User) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := utils.CopyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return filepath.Walk(dst, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	})
}

// enableExtensionForUser appends the extension to the user's enabled list via
// their session bus, so it loads on the next GNOME start. Runs as the user
// (the installer is root), non-interactively.
func enableExtensionForUser(username, uid string) error {
	bus := "unix:path=/run/user/" + uid + "/bus"
	gs := func(args ...string) (string, error) {
		c := exec.Command("sudo", append([]string{"-n", "-u", username, "gsettings"}, args...)...)
		c.Env = append(os.Environ(), "DBUS_SESSION_BUS_ADDRESS="+bus)
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err
	}
	cur, err := gs("get", "org.gnome.shell", "enabled-extensions")
	if err != nil {
		return fmt.Errorf("read enabled-extensions: %w", err)
	}
	next, changed := appendEnabledExtension(cur, gnomeExtUUID)
	if !changed {
		return nil // already enabled
	}
	if _, err := gs("set", "org.gnome.shell", "enabled-extensions", next); err != nil {
		return fmt.Errorf("set enabled-extensions: %w", err)
	}
	return nil
}

// appendEnabledExtension returns the gsettings list value with uuid added and
// whether it changed. Handles the empty forms "@as []" / "[]".
func appendEnabledExtension(cur, uuid string) (string, bool) {
	if strings.Contains(cur, "'"+uuid+"'") {
		return cur, false
	}
	switch strings.TrimSpace(cur) {
	case "@as []", "[]", "":
		return "['" + uuid + "']", true
	default:
		return strings.TrimSuffix(strings.TrimSpace(cur), "]") + ", '" + uuid + "']", true
	}
}
