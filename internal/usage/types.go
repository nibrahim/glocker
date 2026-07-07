// Package usage implements arbtt-style desktop usage tracking: it periodically
// samples which windows are open, their titles, which one is focused, and how
// long the user has been idle, appending each sample to a log for later
// analysis of time-usage patterns.
//
// The package is built around two small interfaces so pieces can be swapped
// without touching the sampling loop:
//
//   - Source captures a Sample from the environment (X11 today; a Wayland or
//     mock Source could be dropped in later).
//   - Sink persists a Sample (JSON Lines today; a database or network sink
//     could be added later).
//
// Tracker ties a Source and a Sink together on a fixed interval.
package usage

import "time"

// Window describes a single window observed in a Sample.
type Window struct {
	// Active reports whether this was the focused window at sample time.
	Active bool `json:"active"`
	// Class is the WM_CLASS class hint (e.g. "firefox", "Alacritty").
	Class string `json:"class"`
	// Instance is the WM_CLASS instance hint (e.g. "Navigator"); omitted when empty.
	Instance string `json:"instance,omitempty"`
	// Title is the window title (_NET_WM_NAME, falling back to WM_NAME).
	Title string `json:"title"`
}

// Sample is one point-in-time observation of the desktop.
type Sample struct {
	// Timestamp is when the sample was taken.
	Timestamp time.Time `json:"ts"`
	// IdleMS is milliseconds since the last user input, or -1 if unknown.
	IdleMS int64 `json:"idle_ms"`
	// Windows lists every window observed, with exactly one (or zero) Active.
	Windows []Window `json:"windows"`
}

// Active returns the focused window in the sample, or nil if none is focused.
func (s Sample) Active() *Window {
	for i := range s.Windows {
		if s.Windows[i].Active {
			return &s.Windows[i]
		}
	}
	return nil
}
