package notify

import "testing"

func TestMailgunDomain(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"noreply@mg.example.com", "mg.example.com", false},
		{"Glocker <noreply@mg.example.com>", "mg.example.com", false},
		{"a@b.c@mg.example.com", "", true}, // ParseAddress rejects a bare double-@ local part
		{"notanemail", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := mailgunDomain(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected an error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}
