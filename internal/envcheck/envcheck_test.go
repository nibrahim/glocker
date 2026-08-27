package envcheck

import "testing"

func TestToolPresentAndMissing(t *testing.T) {
	// `sh` is on every POSIX system the tests run on.
	if r := tool("shell", "sh", "impact"); !r.OK {
		t.Error("sh should be found on PATH")
	}
	if r := tool("bogus", "glocker-definitely-not-a-real-binary", "impact"); r.OK {
		t.Error("a nonexistent binary must not report OK")
	} else if r.Detail == "" {
		t.Error("a failing check must carry an impact detail")
	}
}

func TestWarningsCarryDetail(t *testing.T) {
	// Whatever the environment, every failing check must name an impact so the
	// logged warning is actionable.
	for _, r := range Warnings() {
		if r.OK {
			t.Errorf("Warnings() returned an OK result: %+v", r)
		}
		if r.Detail == "" {
			t.Errorf("warning %q has no detail", r.Name)
		}
	}
}
