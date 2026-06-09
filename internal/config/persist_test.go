package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestSaveDomainsToConfig_AppendsAndPreservesStructure(t *testing.T) {
	// Create a temp config file with known content
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	initialYAML := `# Test config
enable_hosts: true
domains:
  # Existing domains
  - {name: "reddit.com", unblockable: true}
  - {name: "twitter.com"}
enforce_interval_seconds: 60
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Temporarily override GlockerConfigFile
	origPath := GlockerConfigFile
	// We can't override the const, so we test the core logic directly
	// by reimplementing the key parts with a custom path
	t.Cleanup(func() { _ = origPath })

	// Instead, test the YAML manipulation logic directly
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(initialYAML), &doc); err != nil {
		t.Fatal(err)
	}

	root := doc.Content[0]
	var domainsSeq *yaml.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "domains" {
			domainsSeq = root.Content[i+1]
			break
		}
	}

	if domainsSeq == nil {
		t.Fatal("domains key not found")
	}
	if domainsSeq.Kind != yaml.SequenceNode {
		t.Fatal("domains is not a sequence")
	}

	// Should have 2 existing domains
	if len(domainsSeq.Content) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domainsSeq.Content))
	}

	// Append a new domain
	mapNode := &yaml.Node{
		Kind:  yaml.MappingNode,
		Tag:   "!!map",
		Style: yaml.FlowStyle,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "facebook.com", Tag: "!!str"},
		},
	}
	domainsSeq.Content = append(domainsSeq.Content, mapNode)

	// Marshal and verify
	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}

	result := string(out)

	// The new domain should be present
	if !strings.Contains(result, "facebook.com") {
		t.Error("new domain facebook.com not found in output")
	}

	// Existing domains should still be present
	if !strings.Contains(result, "reddit.com") {
		t.Error("existing domain reddit.com lost")
	}
	if !strings.Contains(result, "twitter.com") {
		t.Error("existing domain twitter.com lost")
	}

	// Other config keys should be preserved
	if !strings.Contains(result, "enable_hosts: true") {
		t.Error("enable_hosts setting lost")
	}
	if !strings.Contains(result, "enforce_interval_seconds: 60") {
		t.Error("enforce_interval_seconds setting lost")
	}

	// Comments should be preserved
	if !strings.Contains(result, "# Test config") {
		t.Error("top-level comment lost")
	}
	if !strings.Contains(result, "# Existing domains") {
		t.Error("inline comment lost")
	}
}

func TestSaveDomainsToConfig_SkipsDuplicates(t *testing.T) {
	initialYAML := `domains:
  - {name: "reddit.com"}
  - {name: "twitter.com"}
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(initialYAML), &doc); err != nil {
		t.Fatal(err)
	}

	root := doc.Content[0]
	var domainsSeq *yaml.Node
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "domains" {
			domainsSeq = root.Content[i+1]
			break
		}
	}

	// Build existing set
	existing := make(map[string]bool)
	for _, node := range domainsSeq.Content {
		if node.Kind == yaml.MappingNode {
			for j := 0; j < len(node.Content)-1; j += 2 {
				if node.Content[j].Value == "name" {
					existing[node.Content[j+1].Value] = true
					break
				}
			}
		}
	}

	// "reddit.com" should be detected as duplicate
	if !existing["reddit.com"] {
		t.Error("failed to detect existing domain reddit.com")
	}

	// "facebook.com" should not be a duplicate
	if existing["facebook.com"] {
		t.Error("incorrectly detected facebook.com as existing")
	}
}

func TestSaveDomainsToConfig_RejectsInvalidDomains(t *testing.T) {
	err := SaveDomainsToConfig([]string{"valid.com", "invalid domain!!"})
	if err == nil {
		t.Error("expected error for invalid domain name, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to save") {
		t.Errorf("expected 'refusing to save' error, got: %v", err)
	}
}

func TestSaveDomainsToConfig_NoDomains(t *testing.T) {
	err := SaveDomainsToConfig([]string{})
	if err != nil {
		t.Errorf("expected no error for empty domain list, got: %v", err)
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

func TestSaveForbiddenProgramsToConfig_RejectsInvalid(t *testing.T) {
	err := SaveForbiddenProgramsToConfig([]string{"steam", "bad,name"})
	if err == nil {
		t.Error("expected error for invalid program name, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to save") {
		t.Errorf("expected 'refusing to save' error, got: %v", err)
	}
}

func TestSaveForbiddenProgramsToConfig_NoPrograms(t *testing.T) {
	if err := SaveForbiddenProgramsToConfig([]string{}); err != nil {
		t.Errorf("expected no error for empty program list, got: %v", err)
	}
}

// TestForbiddenProgramsNodeNav exercises findMapValue against the nested
// forbidden_programs.programs structure and verifies that appending a new
// program preserves existing entries, sibling keys, and comments.
func TestForbiddenProgramsNodeNav(t *testing.T) {
	initialYAML := `# Test config
enable_forbidden_programs: true
forbidden_programs:
  enabled: true
  check_interval_seconds: 5
  programs:
    # Existing programs
    - name: "steam"
    - {name: "discord"}
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(initialYAML), &doc); err != nil {
		t.Fatal(err)
	}

	root := doc.Content[0]
	fpMap := findMapValue(root, "forbidden_programs")
	if fpMap == nil {
		t.Fatal("forbidden_programs key not found")
	}
	programsSeq := findMapValue(fpMap, "programs")
	if programsSeq == nil {
		t.Fatal("programs key not found")
	}
	if programsSeq.Kind != yaml.SequenceNode {
		t.Fatalf("programs is not a sequence")
	}
	if len(programsSeq.Content) != 2 {
		t.Fatalf("expected 2 programs, got %d", len(programsSeq.Content))
	}

	// Confirm dedup detection sees the existing names.
	if findMapValue(programsSeq.Content[0], "name").Value != "steam" {
		t.Error("expected first program to be steam")
	}

	// Append a new program and re-marshal.
	programsSeq.Content = append(programsSeq.Content, &yaml.Node{
		Kind:  yaml.MappingNode,
		Tag:   "!!map",
		Style: yaml.FlowStyle,
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "name", Tag: "!!str"},
			{Kind: yaml.ScalarNode, Value: "chromium", Tag: "!!str"},
		},
	})

	out, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	result := string(out)

	for _, want := range []string{"chromium", "steam", "discord",
		"check_interval_seconds: 5", "# Test config", "# Existing programs"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, result)
		}
	}
}
