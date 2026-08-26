package install

import "testing"

func TestAppendEnabledExtension(t *testing.T) {
	const uuid = "glocker-usage@glocker.app"
	cases := []struct {
		name    string
		cur     string
		want    string
		changed bool
	}{
		{"gvariant empty", "@as []", "['glocker-usage@glocker.app']", true},
		{"plain empty", "[]", "['glocker-usage@glocker.app']", true},
		{"blank", "", "['glocker-usage@glocker.app']", true},
		{"one existing other", "['foo@bar']", "['foo@bar', 'glocker-usage@glocker.app']", true},
		{"already present", "['foo@bar', 'glocker-usage@glocker.app']", "['foo@bar', 'glocker-usage@glocker.app']", false},
		{"already present alone", "['glocker-usage@glocker.app']", "['glocker-usage@glocker.app']", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := appendEnabledExtension(c.cur, uuid)
			if got != c.want || changed != c.changed {
				t.Errorf("appendEnabledExtension(%q) = (%q, %v), want (%q, %v)", c.cur, got, changed, c.want, c.changed)
			}
		})
	}
}

func TestParseGnomeMajor(t *testing.T) {
	cases := map[string]int{
		"GNOME Shell 48.7\n": 48,
		"GNOME Shell 45.0":   45,
		"GNOME Shell 3.38.6": 3,
		"garbage":            0,
		"":                   0,
	}
	for in, want := range cases {
		if got := parseGnomeMajor(in); got != want {
			t.Errorf("parseGnomeMajor(%q) = %d, want %d", in, got, want)
		}
	}
}
