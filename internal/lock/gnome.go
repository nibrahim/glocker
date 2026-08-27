// GNOME backend for the screen locker. GNOME/Mutter implements no client lock
// protocol on Wayland (no ext-session-lock-v1, no layer-shell), so instead of
// speaking Wayland we ask the glocker GNOME Shell extension to put up a modal,
// input-grabbing overlay that auto-unlocks on a timer. The extension exposes
// this over the same private session-bus name as the usage bridge.
//
// See extensions/gnome/glocker-usage@glocker.app — the D-Bus contract is:
//   name app.glocker.Usage, path /app/glocker/Usage, interface app.glocker.Lock:
//     LockFor(i seconds) -> b   ·   Unlock()
package lock

import (
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	gnomeBridgeName = "app.glocker.Usage" // well-known bus name the extension owns
	gnomeBridgePath = dbus.ObjectPath("/app/glocker/Usage")
	gnomeLockCall   = "app.glocker.Lock.LockFor"
	gnomeUnlockCall = "app.glocker.Lock.Unlock"
)

// gnomeLocker locks the screen by driving the glocker GNOME Shell extension.
type gnomeLocker struct {
	cfg  Config
	conn *dbus.Conn // owned; closed in Lock

	stopOnce sync.Once
	stop     chan struct{}
}

// newGnome builds a GNOME backend over an existing session-bus connection. It
// takes ownership of conn and closes it when Lock returns.
func newGnome(conn *dbus.Conn, cfg Config) *gnomeLocker {
	return &gnomeLocker{cfg: cfg, conn: conn, stop: make(chan struct{})}
}

// Name identifies the backend in logs.
func (g *gnomeLocker) Name() string { return "gnome" }

// Stop ends the lock early; safe to call from another goroutine.
func (g *gnomeLocker) Stop() { g.stopOnce.Do(func() { close(g.stop) }) }

// Lock asks the shell extension to lock for the configured duration and blocks
// until it elapses (or Stop is called). The extension runs its own unlock timer,
// so the screen unlocks even if this process dies mid-lock.
func (g *gnomeLocker) Lock() error {
	defer g.conn.Close()

	seconds := int32(g.cfg.Duration / time.Second)
	if seconds < 1 {
		seconds = 1
	}

	obj := g.conn.Object(gnomeBridgeName, gnomeBridgePath)
	var ok bool
	if err := obj.Call(gnomeLockCall, 0, seconds).Store(&ok); err != nil {
		return fmt.Errorf("gnome lock (is the glocker GNOME Shell extension enabled?): %w", err)
	}
	if !ok {
		return fmt.Errorf("gnome shell refused the lock (already locked?)")
	}

	select {
	case <-time.After(g.cfg.Duration):
	case <-g.stop:
		// Best-effort early unlock; the extension's own timer is the backstop.
		_ = obj.Call(gnomeUnlockCall, 0).Err
	}
	return nil
}
