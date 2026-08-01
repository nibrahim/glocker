package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// IgnoredEntry marks a single recorded violation as a false positive so the
// dashboard stops counting it. Matching is by ts + keyword + url (the fields
// that uniquely identify one report line); domain is kept only for display.
// The raw reports log is never touched — this is a separate, reversible overlay.
type IgnoredEntry struct {
	TS      int64  `json:"ts"` // event time, epoch milliseconds
	Keyword string `json:"keyword"`
	URL     string `json:"url"`
	Domain  string `json:"domain,omitempty"`
}

// ignoreKey is the identity used to match a violation against the ignore list.
func ignoreKey(ts int64, keyword, url string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", ts, keyword, url)
}

// loadIgnored reads the ignore list, tolerating a missing or corrupt file.
func loadIgnored(path string) ([]IgnoredEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []IgnoredEntry{}, nil
		}
		return []IgnoredEntry{}, err
	}
	var obj struct {
		Ignored []IgnoredEntry `json:"ignored"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return []IgnoredEntry{}, nil // corrupt file -> treat as empty
	}
	return dedupeIgnored(obj.Ignored), nil
}

// loadIgnoredSet returns the ignore list as a lookup set of match keys.
func loadIgnoredSet(path string) map[string]bool {
	list, _ := loadIgnored(path)
	set := make(map[string]bool, len(list))
	for _, e := range list {
		set[ignoreKey(e.TS, e.Keyword, e.URL)] = true
	}
	return set
}

// dedupeIgnored drops entries with no timestamp and collapses duplicates by key,
// preserving the reports-log field values exactly so matching stays reliable.
func dedupeIgnored(in []IgnoredEntry) []IgnoredEntry {
	out := []IgnoredEntry{}
	seen := map[string]bool{}
	for _, e := range in {
		if e.TS == 0 {
			continue
		}
		k := ignoreKey(e.TS, e.Keyword, e.URL)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out
}

// saveIgnored writes the ignore list as pretty JSON, creating parent dirs.
func saveIgnored(in []IgnoredEntry, path string) ([]IgnoredEntry, error) {
	clean := dedupeIgnored(in)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return clean, err
		}
	}
	b, err := json.MarshalIndent(map[string]any{"ignored": clean}, "", "  ")
	if err != nil {
		return clean, err
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return clean, err
	}
	return clean, nil
}
