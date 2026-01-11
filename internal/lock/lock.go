// Package lock implements a timeout-based screen locker for X11.
// Unlike password-based lockers like i3lock, this locker automatically
// unlocks after a configurable timeout period.
package lock

import (
	"fmt"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Locker represents a timeout-based screen locker.
type Locker struct {
	conn     *xgb.Conn
	screen   *xproto.ScreenInfo
	window   xproto.Window
	duration time.Duration
	message  string

	mu       sync.Mutex
	running  bool
	stopChan chan struct{}
}

// Config holds configuration options for the locker.
type Config struct {
	// Duration specifies how long the screen should be locked.
	Duration time.Duration
	// Message is displayed on the lock screen.
	Message string
	// BackgroundColor is the lock screen background (RGB format).
	BackgroundColor uint32
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Duration:        60 * time.Second,
		Message:         "Screen locked",
		BackgroundColor: 0x1a1a2e, // Dark blue
	}
}

// New creates a new Locker instance.
func New(cfg Config) (*Locker, error) {
	if cfg.Duration <= 0 {
		cfg.Duration = DefaultConfig().Duration
	}
	if cfg.Message == "" {
		cfg.Message = DefaultConfig().Message
	}

	return &Locker{
		duration: cfg.Duration,
		message:  cfg.Message,
		stopChan: make(chan struct{}),
	}, nil
}

// Lock activates the screen lock for the configured duration.
// It blocks until the timeout expires or Stop() is called.
func (l *Locker) Lock() error {
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return fmt.Errorf("locker is already running")
	}
	l.running = true
	l.stopChan = make(chan struct{})
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.running = false
		l.mu.Unlock()
	}()

	// Connect to X server
	conn, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("failed to connect to X server: %w", err)
	}
	defer conn.Close()
	l.conn = conn

	// Get the setup info and default screen
	setup := xproto.Setup(conn)
	if len(setup.Roots) == 0 {
		return fmt.Errorf("no screens found")
	}
	l.screen = &setup.Roots[0]

	// Create the lock window
	if err := l.createWindow(); err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	// Grab keyboard and pointer
	if err := l.grabInputs(); err != nil {
		l.destroyWindow()
		return fmt.Errorf("failed to grab inputs: %w", err)
	}

	// Run the lock loop
	return l.runLoop()
}

// Stop terminates the lock screen early.
func (l *Locker) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.running {
		close(l.stopChan)
	}
}

// IsRunning returns whether the locker is currently active.
func (l *Locker) IsRunning() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running
}

// LockFor is a convenience function that locks the screen for the specified duration.
// It blocks until the timeout expires.
func LockFor(duration time.Duration) error {
	return LockWithMessage(duration, "")
}

// LockWithMessage locks the screen for the specified duration with a custom message.
// It blocks until the timeout expires.
func LockWithMessage(duration time.Duration, message string) error {
	locker, err := New(Config{
		Duration: duration,
		Message:  message,
	})
	if err != nil {
		return err
	}
	return locker.Lock()
}

func (l *Locker) createWindow() error {
	// Generate window ID
	wid, err := xproto.NewWindowId(l.conn)
	if err != nil {
		return err
	}
	l.window = wid

	// Create a fullscreen window
	mask := uint32(xproto.CwBackPixel | xproto.CwOverrideRedirect | xproto.CwEventMask)
	values := []uint32{
		0x1a1a2e, // Background color (dark blue)
		1,        // Override redirect (bypass window manager)
		xproto.EventMaskExposure | xproto.EventMaskKeyPress | xproto.EventMaskStructureNotify,
	}

	err = xproto.CreateWindowChecked(
		l.conn,
		l.screen.RootDepth,
		l.window,
		l.screen.Root,
		0, 0, // x, y
		l.screen.WidthInPixels, l.screen.HeightInPixels,
		0,                             // border width
		xproto.WindowClassInputOutput, // class
		l.screen.RootVisual,           // visual
		mask,
		values,
	).Check()
	if err != nil {
		return err
	}

	// Map (show) the window
	if err := xproto.MapWindowChecked(l.conn, l.window).Check(); err != nil {
		return err
	}

	// Raise window to top
	if err := xproto.ConfigureWindowChecked(
		l.conn,
		l.window,
		xproto.ConfigWindowStackMode,
		[]uint32{xproto.StackModeAbove},
	).Check(); err != nil {
		return err
	}

	return nil
}

func (l *Locker) destroyWindow() {
	if l.window != 0 {
		xproto.DestroyWindow(l.conn, l.window)
		l.window = 0
	}
}

func (l *Locker) grabInputs() error {
	// Grab keyboard
	kbGrab, err := xproto.GrabKeyboard(
		l.conn,
		true, // owner_events
		l.screen.Root,
		xproto.TimeCurrentTime,
		xproto.GrabModeAsync,
		xproto.GrabModeAsync,
	).Reply()
	if err != nil {
		return fmt.Errorf("keyboard grab failed: %w", err)
	}
	if kbGrab.Status != xproto.GrabStatusSuccess {
		return fmt.Errorf("keyboard grab unsuccessful: status %d", kbGrab.Status)
	}

	// Grab pointer
	ptrGrab, err := xproto.GrabPointer(
		l.conn,
		true, // owner_events
		l.screen.Root,
		0, // event_mask
		xproto.GrabModeAsync,
		xproto.GrabModeAsync,
		l.window,          // confine_to
		xproto.CursorNone, // cursor
		xproto.TimeCurrentTime,
	).Reply()
	if err != nil {
		xproto.UngrabKeyboard(l.conn, xproto.TimeCurrentTime)
		return fmt.Errorf("pointer grab failed: %w", err)
	}
	if ptrGrab.Status != xproto.GrabStatusSuccess {
		xproto.UngrabKeyboard(l.conn, xproto.TimeCurrentTime)
		return fmt.Errorf("pointer grab unsuccessful: status %d", ptrGrab.Status)
	}

	return nil
}

func (l *Locker) ungrabInputs() {
	xproto.UngrabKeyboard(l.conn, xproto.TimeCurrentTime)
	xproto.UngrabPointer(l.conn, xproto.TimeCurrentTime)
}

func (l *Locker) runLoop() error {
	defer l.ungrabInputs()
	defer l.destroyWindow()

	timer := time.NewTimer(l.duration)
	defer timer.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-l.stopChan:
			return nil
		case <-timer.C:
			return nil
		case <-ticker.C:
			// Update display with remaining time
			remaining := l.duration - time.Since(startTime)
			if remaining < 0 {
				return nil
			}
			l.drawScreen(remaining)

			// Process X events
			l.processEvents()
		}
	}
}

func (l *Locker) processEvents() {
	for {
		ev, err := l.conn.PollForEvent()
		if ev == nil && err == nil {
			return // No more events
		}
		if err != nil {
			return
		}
		// We ignore all events - just consuming them to prevent queue buildup
	}
}

func (l *Locker) drawScreen(remaining time.Duration) {
	// Create a graphics context for drawing
	gc, err := xproto.NewGcontextId(l.conn)
	if err != nil {
		return
	}
	defer xproto.FreeGC(l.conn, gc)

	// White foreground for text
	err = xproto.CreateGCChecked(
		l.conn,
		gc,
		xproto.Drawable(l.window),
		xproto.GcForeground,
		[]uint32{0xffffff},
	).Check()
	if err != nil {
		return
	}

	// Clear the window (redraw background)
	xproto.ClearArea(l.conn, false, l.window, 0, 0, 0, 0)

	// Format remaining time
	secs := int(remaining.Seconds())
	mins := secs / 60
	secs = secs % 60
	timeStr := fmt.Sprintf("%s - Unlocking in %02d:%02d", l.message, mins, secs)

	// Calculate center position (approximate)
	textLen := len(timeStr)
	x := (int(l.screen.WidthInPixels) - textLen*8) / 2 // Approximate 8 pixels per char
	y := int(l.screen.HeightInPixels) / 2

	// Draw text
	xproto.ImageText8(
		l.conn,
		byte(len(timeStr)),
		xproto.Drawable(l.window),
		gc,
		int16(x), int16(y),
		timeStr,
	)
}
