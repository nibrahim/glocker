package lock

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

// Session names a display server the locker can drive.
type Session string

const (
	SessionX11     Session = "x11"
	SessionWayland Session = "wayland"
	SessionUnknown Session = "unknown"
)

// DetectSession reports which display server is in use, preferring Wayland when
// both are present (an X11 backend under XWayland cannot actually hold a lock).
// A nested session inherits XDG_SESSION_TYPE from its parent, so WAYLAND_DISPLAY
// / DISPLAY are the reliable signals.
func DetectSession() Session {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return SessionWayland
	}
	if os.Getenv("DISPLAY") != "" {
		return SessionX11
	}
	// Fall back to the session-type hint if neither socket var is set.
	switch os.Getenv("XDG_SESSION_TYPE") {
	case "wayland":
		return SessionWayland
	case "x11":
		return SessionX11
	}
	return SessionUnknown
}

// Select returns a locker Backend appropriate for the current session. Defaults
// are applied to cfg first, so callers can pass a partially-filled Config.
func Select(cfg Config) (Backend, error) {
	cfg = withDefaults(cfg)
	switch DetectSession() {
	case SessionWayland:
		return selectWayland(cfg)
	case SessionX11:
		return newX11(cfg)
	default:
		return nil, fmt.Errorf("no graphical session detected (set WAYLAND_DISPLAY or DISPLAY)")
	}
}

// selectWayland sub-selects a Wayland backend by compositor. GNOME/Mutter has no
// client lock protocol, so there it drives the glocker GNOME Shell extension
// (gnome backend); wlroots and KDE get the standard ext-session-lock backend.
// GNOME is detected by GNOME Shell owning its bus name — more reliable than
// XDG_CURRENT_DESKTOP, which a nested session inherits from its parent.
func selectWayland(cfg Config) (Backend, error) {
	conn, err := dbus.ConnectSessionBus()
	if err == nil {
		if nameHasOwner(conn, "org.gnome.Shell") {
			if nameHasOwner(conn, gnomeBridgeName) {
				return newGnome(conn, cfg), nil // gnome backend owns conn
			}
			conn.Close()
			return nil, fmt.Errorf("GNOME/Wayland has no client lock protocol; enable the glocker GNOME Shell extension, which provides the lock (see extensions/gnome)")
		}
		conn.Close()
	}
	// Non-GNOME (wlroots/KDE) or no session bus: use ext-session-lock.
	return newWayland(cfg)
}

// nameHasOwner reports whether a well-known name is currently owned on conn.
func nameHasOwner(conn *dbus.Conn, name string) bool {
	var has bool
	err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&has)
	return err == nil && has
}
