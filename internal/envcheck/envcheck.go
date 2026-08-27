// Package envcheck verifies that the machine actually supports the mechanisms
// glocker relies on — systemd, visudo, a cron daemon, and a filesystem that can
// hold the immutable flag. When one is missing, the matching feature silently
// no-ops (e.g. tamper protection is off, or the watchdog never runs), so this
// surfaces a clear warning at install and at daemon startup instead.
//
// It is read-only apart from a tiny throwaway file used to probe immutability,
// which is removed immediately.
package envcheck

import (
	"os"
	"os/exec"
)

// Result is one capability check.
type Result struct {
	Name   string
	OK     bool
	Detail string // impact, set only when OK is false
}

// Check runs every capability probe and returns all results.
func Check() []Result {
	return []Result{
		tool("systemd", "systemctl", "the glocker service can't be installed or auto-started"),
		tool("visudo", "visudo", "sudoers changes can't be validated before they're applied"),
		cron(),
		immutable(),
	}
}

// Warnings returns only the checks that failed.
func Warnings() []Result {
	var w []Result
	for _, r := range Check() {
		if !r.OK {
			w = append(w, r)
		}
	}
	return w
}

// LogWarnings prints any failing checks through logf (e.g. log.Printf). It's a
// no-op when everything is supported.
func LogWarnings(logf func(string, ...any)) {
	w := Warnings()
	if len(w) == 0 {
		return
	}
	logf("⚠ Environment check — some capabilities are missing:")
	for _, r := range w {
		logf("   • %s — %s", r.Name, r.Detail)
	}
}

func tool(name, bin, impact string) Result {
	if _, err := exec.LookPath(bin); err != nil {
		return Result{Name: name, Detail: impact}
	}
	return Result{Name: name, OK: true}
}

func cron() Result {
	for _, bin := range []string{"cron", "crond"} {
		if _, err := exec.LookPath(bin); err == nil {
			return Result{Name: "cron", OK: true}
		}
	}
	return Result{Name: "cron", Detail: "the glockdoc heartbeat watchdog won't run"}
}

// immutable probes whether the filesystem under /etc really supports `chattr +i`
// — glocker uses it to protect /etc/hosts, its config, and its binaries. The
// only reliable test is to try it on a throwaway file (removed immediately).
func immutable() Result {
	r := Result{Name: "immutable flag (chattr +i)"}
	if _, err := exec.LookPath("chattr"); err != nil {
		r.Detail = "chattr not found — tamper protection will be inactive"
		return r
	}
	f, err := os.CreateTemp("/etc", ".glocker-fscheck-*")
	if err != nil {
		r.Detail = "couldn't write a probe file to /etc (need root): " + err.Error()
		return r
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if err := exec.Command("chattr", "+i", path).Run(); err != nil {
		r.Detail = "this filesystem can't hold the immutable flag — tamper protection will be inactive"
		return r
	}
	_ = exec.Command("chattr", "-i", path).Run() // must clear before the deferred remove
	r.OK = true
	return r
}
