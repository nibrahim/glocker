package config

import (
	"strings"
	"testing"
)

func TestValidateDomainName(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"valid simple", "example.com", false},
		{"valid subdomain", "www.example.com", false},
		{"valid with hyphen", "my-site.example.com", false},
		{"valid single label", "localhost", false},
		{"empty", "", true},
		{"too long", strings.Repeat("a", 254), true},
		{"has space", "example .com", true},
		{"has newline", "example.com\nevil: true", true},
		{"has colon", "example.com:8080", true},
		{"has slash", "example.com/path", true},
		{"starts with hyphen", "-example.com", true},
		{"ends with hyphen", "example-.com", true},
		{"has underscore", "ex_ample.com", true},
		{"yaml injection attempt", "evil.com}\ndomains: []", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDomainName(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomainName(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProgramName(t *testing.T) {
	tests := []struct {
		name    string
		program string
		wantErr bool
	}{
		{"valid simple", "steam", false},
		{"valid with dash", "mullvad-browser", false},
		{"valid binary basename", "FTL.amd64", false},
		{"empty", "", true},
		{"too long", strings.Repeat("a", 65), true},
		{"has colon", "fire:fox", true},
		{"has comma", "firefox,steam", true},
		{"has newline", "firefox\nevil: true", true},
		{"has tab", "fire\tfox", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProgramName(tt.program)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProgramName(%q) error = %v, wantErr %v", tt.program, err, tt.wantErr)
			}
		})
	}
}
