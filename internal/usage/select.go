package usage

import (
	"errors"
	"fmt"
	"os"
	"runtime"
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

// sessionKind is "x11", "wayland", or "" (unknown), for the current Linux
// session. XDG_SESSION_TYPE is authoritative; the *_DISPLAY vars are a fallback.
// Wayland is checked first because a Wayland session usually also exports DISPLAY
// (XWayland) — using the X11 backend there would see only XWayland clients, not
// native Wayland windows.
func detectLinuxSession(opts Options) string {
	switch os.Getenv("XDG_SESSION_TYPE") {
	case "wayland":
		return "wayland"
	case "x11":
		return "x11"
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return "wayland"
	}
	if opts.Display != "" || os.Getenv("DISPLAY") != "" {
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
		return nil, "", fmt.Errorf("%w: Wayland session (%s) has no backend yet",
			ErrUnsupportedSession, envOr("XDG_CURRENT_DESKTOP", "unknown"))
	default:
		return nil, "", fmt.Errorf("%w: no X11 or Wayland session detected", ErrUnsupportedSession)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
