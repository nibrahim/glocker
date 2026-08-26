// Glocker Usage — GNOME Shell extension (GNOME 45+, ES modules).
//
// GNOME/Mutter blocks apps from querying the window list on Wayland
// (org.gnome.Shell.Introspect and Eval are both access-denied). Running inside
// the shell, we *can* see it, so we re-expose it over a private session-bus
// name that glocker's usage tracker reads.
//
// D-Bus CONTRACT — keep this identical across every shell-version build so the
// Go side depends only on the contract, never on the GNOME version:
//   name:      app.glocker.Usage
//   path:      /app/glocker/Usage
//   method:    GetWindows() -> s   (JSON array of {class, instance, title, active})
//
// Only stable Meta/global APIs are used, so bumping shell-version for a new
// GNOME release needs no code change.

import Gio from 'gi://Gio';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const BUS_NAME = 'app.glocker.Usage';
const OBJECT_PATH = '/app/glocker/Usage';
const IFACE = `
<node>
  <interface name="app.glocker.Usage">
    <method name="GetWindows">
      <arg type="s" direction="out" name="json"/>
    </method>
  </interface>
</node>`;

export default class GlockerUsageExtension extends Extension {
    enable() {
        this._impl = Gio.DBusExportedObject.wrapJSObject(IFACE, this);
        this._impl.export(Gio.DBus.session, OBJECT_PATH);
        this._nameId = Gio.DBus.session.own_name(
            BUS_NAME, Gio.BusNameOwnerFlags.NONE, null, null);
    }

    disable() {
        if (this._nameId) {
            Gio.DBus.session.unown_name(this._nameId);
            this._nameId = null;
        }
        if (this._impl) {
            this._impl.unexport();
            this._impl = null;
        }
    }

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
}
