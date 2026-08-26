package usage

import "testing"

func TestParseGnomeWindows(t *testing.T) {
	// The exact shape the extension returns (captured from a live GNOME 48 shell).
	raw := `[{"class":"org.gnome.Terminal","instance":"org.gnome.Terminal","title":"Terminal","active":true},` +
		`{"class":"firefox","instance":"Navigator","title":"News","active":false}]`

	ws, err := parseGnomeWindows(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("got %d windows, want 2", len(ws))
	}
	if ws[0].Class != "org.gnome.Terminal" || ws[0].Title != "Terminal" || !ws[0].Active {
		t.Errorf("window0 = %+v", ws[0])
	}
	if ws[1].Instance != "Navigator" || ws[1].Active {
		t.Errorf("window1 = %+v", ws[1])
	}
	// The Sample.Active() helper should find the focused one.
	if a := (Sample{Windows: ws}).Active(); a == nil || a.Class != "org.gnome.Terminal" {
		t.Errorf("Active() = %+v", a)
	}
}

func TestParseGnomeWindowsEmptyAndBad(t *testing.T) {
	if ws, err := parseGnomeWindows("[]"); err != nil || len(ws) != 0 {
		t.Errorf("empty list: ws=%v err=%v", ws, err)
	}
	if _, err := parseGnomeWindows("not json"); err == nil {
		t.Error("expected an error on malformed JSON")
	}
}
