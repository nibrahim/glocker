package stats

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Rule is one categorization rule: optional program/title regexes -> tag.
// Mirrors the client-side model and glockpeek-web/lib/rules.js.
type Rule struct {
	Program string `json:"program"`
	Title   string `json:"title"`
	Tag     string `json:"tag"`
}

// rulesConfig is the persisted usage config: rules plus a per-tag colour map.
type rulesConfig struct {
	Rules  []Rule            `json:"rules"`
	Colors map[string]string `json:"colors"`
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// sanitizeRules drops rules with no tag and trims fields; never returns nil so
// the JSON is always an array.
func sanitizeRules(in []Rule) []Rule {
	out := []Rule{}
	for _, r := range in {
		tag := strings.TrimSpace(r.Tag)
		if tag == "" {
			continue
		}
		out = append(out, Rule{
			Program: strings.TrimSpace(r.Program),
			Title:   strings.TrimSpace(r.Title),
			Tag:     tag,
		})
	}
	return out
}

// sanitizeColors keeps only tag -> #rrggbb entries; never returns nil.
func sanitizeColors(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if strings.TrimSpace(k) != "" && hexColor.MatchString(v) {
			out[k] = strings.ToLower(v)
		}
	}
	return out
}

// loadConfig reads the config, tolerating a missing/corrupt file and the legacy
// bare-array format.
func loadConfig(path string) (rulesConfig, error) {
	cfg := rulesConfig{Rules: []Rule{}, Colors: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	cfg.Rules, cfg.Colors = parseConfigBytes(data)
	return cfg, nil
}

// parseConfigBytes decodes either a { rules, colors } object or a legacy bare
// rules array, sanitizing the result. A corrupt payload yields empty config.
func parseConfigBytes(data []byte) ([]Rule, map[string]string) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return []Rule{}, map[string]string{}
	}
	if trimmed[0] == '[' {
		var arr []Rule
		_ = json.Unmarshal(trimmed, &arr)
		return sanitizeRules(arr), map[string]string{}
	}
	var obj rulesConfig
	_ = json.Unmarshal(trimmed, &obj)
	return sanitizeRules(obj.Rules), sanitizeColors(obj.Colors)
}

// saveConfig sanitizes and writes the config as pretty JSON, creating parent
// dirs as needed.
func saveConfig(in rulesConfig, path string) (rulesConfig, error) {
	clean := rulesConfig{Rules: sanitizeRules(in.Rules), Colors: sanitizeColors(in.Colors)}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return clean, err
		}
	}
	b, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return clean, err
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return clean, err
	}
	return clean, nil
}
