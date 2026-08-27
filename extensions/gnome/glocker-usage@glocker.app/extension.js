// Glocker shell bridge — GNOME Shell extension (GNOME 45+, ES modules).
//
// GNOME/Mutter denies apps two things glocker needs on Wayland, both of which
// the shell itself can do. Running inside the shell, we re-expose them over a
// private session-bus name that glocker reads:
//
//   1. The window list — Mutter blocks org.gnome.Shell.Introspect/Eval, so the
//      usage tracker can't see focused/open windows on its own.
//   2. A screen lock — Mutter implements no client lock protocol on Wayland (no
//      ext-session-lock-v1, no layer-shell), so glocklock can't lock the screen.
//      Here we put up a modal, input-grabbing overlay that auto-unlocks on a
//      timer — glocklock's timed-lock model, done shell-side.
//
// D-Bus CONTRACT — keep identical across every shell-version build so the Go
// side depends only on the contract, never on the GNOME version:
//   name: app.glocker.Usage   path: /app/glocker/Usage
//   interface app.glocker.Usage:
//     GetWindows() -> s        JSON array of {class, instance, title, active}
//   interface app.glocker.Lock:
//     LockFor(i seconds, s image, s message) -> b
//                              put up the timed lock (optional full-screen image
//                              + heading); false if already locked
//     Unlock()                 release the lock early
//
// Only stable Meta/Main/St/Clutter APIs are used, so bumping shell-version for a
// new GNOME release needs no code change.

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';
import Clutter from 'gi://Clutter';
import Shell from 'gi://Shell';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

const BUS_NAME = 'app.glocker.Usage';
const OBJECT_PATH = '/app/glocker/Usage';

const USAGE_IFACE = `
<node>
  <interface name="app.glocker.Usage">
    <method name="GetWindows">
      <arg type="s" direction="out" name="json"/>
    </method>
  </interface>
</node>`;

const LOCK_IFACE = `
<node>
  <interface name="app.glocker.Lock">
    <method name="LockFor">
      <arg type="i" direction="in" name="seconds"/>
      <arg type="s" direction="in" name="image"/>
      <arg type="s" direction="in" name="message"/>
      <arg type="b" direction="out" name="ok"/>
    </method>
    <method name="Unlock"/>
  </interface>
</node>`;

export default class GlockerBridgeExtension extends Extension {
    enable() {
        // Two interfaces, same object + JS instance: GetWindows / LockFor+Unlock.
        this._usageImpl = Gio.DBusExportedObject.wrapJSObject(USAGE_IFACE, this);
        this._usageImpl.export(Gio.DBus.session, OBJECT_PATH);
        this._lockImpl = Gio.DBusExportedObject.wrapJSObject(LOCK_IFACE, this);
        this._lockImpl.export(Gio.DBus.session, OBJECT_PATH);
        this._nameId = Gio.DBus.session.own_name(
            BUS_NAME, Gio.BusNameOwnerFlags.NONE, null, null);

        // Lock state.
        this._lockActor = null;
        this._grab = null;
        this._tick = 0;
    }

    disable() {
        this._unlock(); // never leave the screen grabbed if the extension unloads
        if (this._nameId) {
            Gio.DBus.session.unown_name(this._nameId);
            this._nameId = null;
        }
        if (this._usageImpl) {
            this._usageImpl.unexport();
            this._usageImpl = null;
        }
        if (this._lockImpl) {
            this._lockImpl.unexport();
            this._lockImpl = null;
        }
    }

    // ── app.glocker.Usage ──────────────────────────────────────────────────

    // GetWindows returns every managed window as JSON, with the focused one
    // marked active — the shape glocker's usage.Sample expects.
    GetWindows() {
        const focus = global.display.get_focus_window();
        const windows = global.get_window_actors()
            .map(actor => actor.meta_window)
            .filter(w => w)
            .map(w => ({
                class: w.get_wm_class() ?? '',
                instance: w.get_wm_class_instance() ?? '',
                title: w.get_title() ?? '',
                active: focus !== null && w === focus,
            }));
        return JSON.stringify(windows);
    }

    // ── app.glocker.Lock ───────────────────────────────────────────────────

    // LockFor puts up a full-screen modal overlay that grabs all input and
    // unlocks itself after `seconds`. An optional background image fills the
    // screen (behind the countdown); message prefixes the countdown text.
    // Returns false if already locked or the grab was refused. glocklock's gnome
    // backend calls this and waits out the same duration.
    LockFor(seconds, image, message) {
        if (this._lockActor)
            return false;
        if (seconds < 1)
            seconds = 1;

        let style = 'background-color: #1a3d2e;';
        if (image && image.length > 0) {
            style += ` background-image: url("file://${image}");` +
                ' background-size: cover; background-position: center;';
        }
        const actor = new St.Widget({
            reactive: true,
            can_focus: true,
            track_hover: true,
            layout_manager: new Clutter.BinLayout(),
            x: 0,
            y: 0,
            width: global.stage.width,
            height: global.stage.height,
            style,
        });
        // A dark pill behind the text keeps it readable over any image.
        const label = new St.Label({
            x_align: Clutter.ActorAlign.CENTER,
            y_align: Clutter.ActorAlign.CENTER,
            style: 'color: #ffffff; font-size: 22px; text-align: center; ' +
                'padding: 18px 28px; border-radius: 14px; ' +
                'background-color: rgba(0, 0, 0, 0.55);',
        });
        actor.add_child(label);
        const heading = (message && message.length > 0) ? message : 'Screen locked';
        // Swallow every key/button so nothing reaches apps beneath.
        actor.connect('key-press-event', () => Clutter.EVENT_STOP);
        actor.connect('button-press-event', () => Clutter.EVENT_STOP);

        Main.layoutManager.uiGroup.add_child(actor);

        // actionMode NONE disables shell keybindings (Super, overview, etc.).
        const grab = Main.pushModal(actor, {actionMode: Shell.ActionMode.NONE});
        if (!grab) {
            actor.destroy();
            return false;
        }

        this._lockActor = actor;
        this._grab = grab;

        let remaining = seconds;
        const render = () => {
            const m = Math.floor(remaining / 60);
            const s = remaining % 60;
            label.text = `${heading}\nUnlocking in ${m}:${String(s).padStart(2, '0')}`;
        };
        render();
        this._tick = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
            remaining -= 1;
            if (remaining <= 0) {
                this._unlock();
                return GLib.SOURCE_REMOVE;
            }
            render();
            return GLib.SOURCE_CONTINUE;
        });
        return true;
    }

    // Unlock releases the lock early (used by the backend's Stop()).
    Unlock() {
        this._unlock();
    }

    _unlock() {
        if (this._tick) {
            GLib.source_remove(this._tick);
            this._tick = 0;
        }
        if (this._grab) {
            Main.popModal(this._grab);
            this._grab = null;
        }
        if (this._lockActor) {
            this._lockActor.destroy();
            this._lockActor = null;
        }
    }
}
