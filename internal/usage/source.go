package usage

// Source captures point-in-time samples of desktop window state.
//
// Implementations wrap a specific windowing system. X11Source is the only one
// today, but a Wayland source, a replay/file source, or a mock (for tests)
// can implement the same interface without any change to Tracker.
type Source interface {
	// Capture returns a single Sample of the current desktop state.
	Capture() (Sample, error)
	// Close releases any resources (e.g. the X server connection).
	Close() error
}
