package lock

import (
	"fmt"
	"os"
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
		return newWayland(cfg)
	case SessionX11:
		return newX11(cfg)
	default:
		return nil, fmt.Errorf("no graphical session detected (set WAYLAND_DISPLAY or DISPLAY)")
	}
}
