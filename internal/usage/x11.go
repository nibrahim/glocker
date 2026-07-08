package usage

import (
	"fmt"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

// maxPropLen bounds how many 32-bit units of a property we read. Titles and
// client lists are small; this is just a sanity ceiling.
const maxPropLen = 1 << 20

// X11Source captures window state from an X11 display using EWMH hints
// (_NET_ACTIVE_WINDOW, _NET_CLIENT_LIST, _NET_WM_NAME) plus the
// MIT-SCREEN-SAVER extension for idle time.
type X11Source struct {
	conn *xgb.Conn
	root xproto.Window

	atomActiveWindow xproto.Atom
	atomClientList   xproto.Atom
	atomNetWMName    xproto.Atom
	atomWMName       xproto.Atom
	atomWMClass      xproto.Atom

	haveScreensaver bool
}

// NewX11Source connects to the X server named by $DISPLAY and prepares the
// atoms and extensions needed for sampling.
func NewX11Source() (*X11Source, error) {
	return NewX11SourceDisplay("")
}

// NewX11SourceDisplay is like NewX11Source but connects to an explicit display
// (e.g. ":0"); an empty display falls back to $DISPLAY. Useful when the tracker
// runs outside the user's session (e.g. the root daemon).
func NewX11SourceDisplay(display string) (*X11Source, error) {
	var conn *xgb.Conn
	var err error
	if display == "" {
		conn, err = xgb.NewConn()
	} else {
		conn, err = xgb.NewConnDisplay(display)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to X server: %w", err)
	}
	setup := xproto.Setup(conn)
	if len(setup.Roots) == 0 {
		conn.Close()
		return nil, fmt.Errorf("no X screens found")
	}
	s := &X11Source{conn: conn, root: setup.Roots[0].Root}

	atoms := []struct {
		name string
		dst  *xproto.Atom
	}{
		{"_NET_ACTIVE_WINDOW", &s.atomActiveWindow},
		{"_NET_CLIENT_LIST", &s.atomClientList},
		{"_NET_WM_NAME", &s.atomNetWMName},
		{"WM_NAME", &s.atomWMName},
		{"WM_CLASS", &s.atomWMClass},
	}
	for _, a := range atoms {
		atom, err := internAtom(conn, a.name)
		if err != nil {
			conn.Close()
			return nil, err
		}
		*a.dst = atom
	}

	// Idle detection is best-effort: if the extension is missing we still
	// capture windows, just with IdleMS == -1.
	if err := screensaver.Init(conn); err == nil {
		s.haveScreensaver = true
	}

	return s, nil
}

// Capture reads the active window, the full client list, and idle time.
func (s *X11Source) Capture() (Sample, error) {
	sample := Sample{Timestamp: time.Now(), IdleMS: -1}

	if s.haveScreensaver {
		if idle, err := s.idleMS(); err == nil {
			sample.IdleMS = idle
		}
	}

	active, _ := s.activeWindow() // 0 if none/error; simply matches no window

	ids, err := s.clientList()
	if err != nil {
		return sample, fmt.Errorf("read client list: %w", err)
	}
	for _, id := range ids {
		w := Window{Active: id == active, Title: s.windowTitle(id)}
		w.Instance, w.Class = s.windowClass(id)
		sample.Windows = append(sample.Windows, w)
	}
	return sample, nil
}

// Close closes the X server connection.
func (s *X11Source) Close() error {
	s.conn.Close()
	return nil
}

func (s *X11Source) idleMS() (int64, error) {
	info, err := screensaver.QueryInfo(s.conn, xproto.Drawable(s.root)).Reply()
	if err != nil {
		return -1, err
	}
	return int64(info.MsSinceUserInput), nil
}

func (s *X11Source) activeWindow() (xproto.Window, error) {
	reply, err := xproto.GetProperty(s.conn, false, s.root, s.atomActiveWindow,
		xproto.AtomWindow, 0, 1).Reply()
	if err != nil {
		return 0, err
	}
	if reply == nil || reply.Format != 32 || len(reply.Value) < 4 {
		return 0, fmt.Errorf("no active window")
	}
	return xproto.Window(xgb.Get32(reply.Value)), nil
}

func (s *X11Source) clientList() ([]xproto.Window, error) {
	reply, err := xproto.GetProperty(s.conn, false, s.root, s.atomClientList,
		xproto.AtomWindow, 0, maxPropLen).Reply()
	if err != nil {
		return nil, err
	}
	if reply == nil {
		return nil, nil
	}
	n := int(reply.ValueLen)
	out := make([]xproto.Window, 0, n)
	for i := 0; i < n && (i+1)*4 <= len(reply.Value); i++ {
		out = append(out, xproto.Window(xgb.Get32(reply.Value[i*4:])))
	}
	return out, nil
}

// windowTitle prefers the EWMH UTF-8 _NET_WM_NAME, falling back to the legacy
// WM_NAME.
func (s *X11Source) windowTitle(w xproto.Window) string {
	if t := s.propString(w, s.atomNetWMName); t != "" {
		return t
	}
	return s.propString(w, s.atomWMName)
}

// propString reads a text property of any string type as a Go string.
func (s *X11Source) propString(w xproto.Window, prop xproto.Atom) string {
	reply, err := xproto.GetProperty(s.conn, false, w, prop,
		xproto.GetPropertyTypeAny, 0, maxPropLen).Reply()
	if err != nil || reply == nil {
		return ""
	}
	return string(reply.Value)
}

// windowClass parses WM_CLASS, two consecutive null-terminated strings:
// instance then class.
func (s *X11Source) windowClass(w xproto.Window) (instance, class string) {
	reply, err := xproto.GetProperty(s.conn, false, w, s.atomWMClass,
		xproto.AtomString, 0, maxPropLen).Reply()
	if err != nil || reply == nil || len(reply.Value) == 0 {
		return "", ""
	}
	parts := strings.Split(strings.TrimRight(string(reply.Value), "\x00"), "\x00")
	if len(parts) > 0 {
		instance = parts[0]
	}
	if len(parts) > 1 {
		class = parts[1]
	}
	return instance, class
}

func internAtom(conn *xgb.Conn, name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(conn, true, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("intern atom %q: %w", name, err)
	}
	return reply.Atom, nil
}
