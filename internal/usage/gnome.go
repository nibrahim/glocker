package usage

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// GNOMESource reads window state on GNOME/Wayland. Mutter blocks apps from
// querying windows directly (org.gnome.Shell.Introspect and Eval are
// access-denied), so this reads two session-bus interfaces:
//
//   - idle time from org.gnome.Mutter.IdleMonitor (always available), and
//   - the window list from the glocker GNOME Shell extension's private name
//     app.glocker.Usage (must be installed + enabled — see extensions/gnome).
//
// It uses the session bus from the environment, so it works when run in the
// user's session (e.g. usageprobe). Reaching a user's bus from the root daemon
// needs that address plumbed through, like X11's XAUTHORITY — a later step.
type GNOMESource struct {
	conn *dbus.Conn
}

const (
	gnomeIdleDest = "org.gnome.Mutter.IdleMonitor"
	gnomeIdlePath = dbus.ObjectPath("/org/gnome/Mutter/IdleMonitor/Core")
	gnomeIdleCall = "org.gnome.Mutter.IdleMonitor.GetIdletime"

	glockerBridgeDest = "app.glocker.Usage"
	glockerBridgePath = dbus.ObjectPath("/app/glocker/Usage")
	glockerBridgeCall = "app.glocker.Usage.GetWindows"
)

// NewGNOMESource wraps an existing session-bus connection. The source takes
// ownership of conn and closes it in Close. The caller (the selector) has
// already verified the extension bridge is present.
func NewGNOMESource(conn *dbus.Conn) *GNOMESource {
	return &GNOMESource{conn: conn}
}

// Close releases the session-bus connection.
func (s *GNOMESource) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *GNOMESource) Capture() (Sample, error) {
	windows, err := s.windows()
	if err != nil {
		return Sample{}, err
	}
	return Sample{
		Timestamp: time.Now(),
		IdleMS:    s.idleMS(), // best-effort; -1 if the idle monitor can't be read
		Windows:   windows,
	}, nil
}

// idleMS returns milliseconds since the last input, or -1 if unavailable (idle
// is non-essential — a missing value shouldn't fail an otherwise-good capture).
func (s *GNOMESource) idleMS() int64 {
	var ms uint64
	if err := s.conn.Object(gnomeIdleDest, gnomeIdlePath).Call(gnomeIdleCall, 0).Store(&ms); err != nil {
		return -1
	}
	return int64(ms)
}

// windows reads the window list from the glocker extension's D-Bus bridge.
func (s *GNOMESource) windows() ([]Window, error) {
	var raw string
	err := s.conn.Object(glockerBridgeDest, glockerBridgePath).Call(glockerBridgeCall, 0).Store(&raw)
	if err != nil {
		return nil, fmt.Errorf("gnome: window bridge unavailable — is the glocker GNOME Shell extension installed and enabled? (%w)", err)
	}
	return parseGnomeWindows(raw)
}

// parseGnomeWindows decodes the extension's JSON: [{class,instance,title,active}].
func parseGnomeWindows(raw string) ([]Window, error) {
	var windows []Window
	if err := json.Unmarshal([]byte(raw), &windows); err != nil {
		return nil, fmt.Errorf("gnome: bad window JSON from extension: %w", err)
	}
	return windows, nil
}
