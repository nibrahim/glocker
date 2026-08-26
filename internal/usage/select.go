package usage

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/godbus/dbus/v5"
)

// ErrUnsupportedSession means no usage backend fits the current session — a
// Wayland session before its backend exists, an unknown/headless session, or a
// non-Linux OS. Callers should treat it as "usage tracking unavailable here" and
// carry on, not fail.
var ErrUnsupportedSession = errors.New("usage: unsupported session")

// Options configures source selection. Fields are backend-specific and ignored
// by backends that don't use them (Display/XAuthority are X11-only).
type Options struct {
	Display    string // X11 DISPLAY override (empty = $DISPLAY)
	XAuthority string // X11 XAUTHORITY override (empty = inherit)
}

// NewSource picks a Source for the current desktop session and returns it with a
// short backend name (e.g. "linux/x11") for logging. It is the single place that
// maps an environment to a backend, hiding the OS and compositor behind the
// Source interface.
//
// Today: Linux/X11 is implemented. A Wayland session is detected but returns
// ErrUnsupportedSession until its backend lands (and other OSes are future work
// — add a case to the switch). Detecting the specific Wayland compositor
// (wlroots/GNOME/KDE) will live inside the Wayland source, not here.
func NewSource(opts Options) (Source, string, error) {
	switch runtime.GOOS {
	case "linux":
		return newLinuxSource(opts)
	default:
		return nil, "", fmt.Errorf("%w: %s is not supported", ErrUnsupportedSession, runtime.GOOS)
	}
}

// detectLinuxSession returns "x11", "wayland", or "" (unknown) for the current
// Linux session.
//
// A reachable Wayland compositor (WAYLAND_DISPLAY) is the strongest signal and
// is checked first: it's set even in a nested Wayland-in-X11 session, where
// XDG_SESSION_TYPE still reports the *outer* "x11" and DISPLAY points at
// XWayland — using the X11 backend there would see the wrong (or no) windows.
// Only when there's no Wayland socket do we fall back to XDG_SESSION_TYPE /
// DISPLAY for X11.
func detectLinuxSession(opts Options) string {
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "wayland" {
		return "wayland"
	}
	if opts.Display != "" || os.Getenv("DISPLAY") != "" || os.Getenv("XDG_SESSION_TYPE") == "x11" {
		return "x11"
	}
	return ""
}

func newLinuxSource(opts Options) (Source, string, error) {
	if opts.XAuthority != "" {
		os.Setenv("XAUTHORITY", opts.XAuthority)
	}
	switch detectLinuxSession(opts) {
	case "x11":
		src, err := NewX11SourceDisplay(opts.Display)
		return src, "linux/x11", err
	case "wayland":
		// Sub-select by compositor. GNOME is detected by GNOME Shell being on the
		// session bus (more reliable than XDG_CURRENT_DESKTOP, which a nested
		// session inherits from its parent). wlroots/KDE come later.
		if gnomeSessionPresent() {
			src, err := NewGNOMESource()
			return src, "linux/wayland/gnome", err
		}
		return nil, "", fmt.Errorf("%w: Wayland session (%s) has no supported backend yet",
			ErrUnsupportedSession, envOr("XDG_CURRENT_DESKTOP", "unknown"))
	default:
		return nil, "", fmt.Errorf("%w: no X11 or Wayland session detected", ErrUnsupportedSession)
	}
}

// gnomeSessionPresent reports whether GNOME Shell is on the session bus.
func gnomeSessionPresent() bool {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.gnome.Shell").Store(&has)
	return err == nil && has
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
