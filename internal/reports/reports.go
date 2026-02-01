// Package reports provides parsing and querying of glocker log files.
package reports

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"
)

// Default log file paths
const (
	DefaultUnblocksLogPath   = "/var/log/glocker-unblocks.log"
	DefaultReportsLogPath    = "/var/log/glocker-reports.log"
	DefaultLifecycleLogPath  = "/var/log/glocker-lifecycle.log"
)

// UnblockEntry represents a single unblock log entry.
type UnblockEntry struct {
	UnblockTime time.Time `json:"unblock_time"`
	RestoreTime time.Time `json:"restore_time"`
	Reason      string    `json:"reason"`
	Domain      string    `json:"domain"`
}

// ReportType indicates whether a report was triggered by URL or content keyword.
type ReportType string

const (
	ReportTypeURL     ReportType = "url-keyword"
	ReportTypeContent ReportType = "content-keyword"
)

// ReportEntry represents a single content/URL report entry.
type ReportEntry struct {
	Timestamp  time.Time
	Type       ReportType
	Keyword    string
	URL        string
	Domain     string
}

// ParseUnblocksLog reads and parses the unblocks log file.
func ParseUnblocksLog(path string) ([]UnblockEntry, error) {
	if path == "" {
		path = DefaultUnblocksLogPath
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []UnblockEntry
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry UnblockEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return entries, err
	}

	return entries, nil
}

// reportLineRegex matches: [2025-11-17 15:35:46] | type:keyword | url | domain
var reportLineRegex = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\] \| (url-keyword|content-keyword):([^ |]+) \| ([^ |]+)(?: \| (.+))?$`)

// ParseReportsLog reads and parses the reports log file.
func ParseReportsLog(path string) ([]ReportEntry, error) {
	if path == "" {
		path = DefaultReportsLogPath
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []ReportEntry
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		entry, ok := parseReportLine(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return entries, err
	}

	return entries, nil
}

func parseReportLine(line string) (ReportEntry, bool) {
	matches := reportLineRegex.FindStringSubmatch(line)
	if matches == nil {
		return ReportEntry{}, false
	}

	timestamp, err := time.ParseInLocation("2006-01-02 15:04:05", matches[1], time.Local)
	if err != nil {
		return ReportEntry{}, false
	}

	entry := ReportEntry{
		Timestamp: timestamp,
		Type:      ReportType(matches[2]),
		Keyword:   matches[3],
		URL:       matches[4],
	}

	if len(matches) > 5 && matches[5] != "" {
		entry.Domain = matches[5]
	}

	return entry, true
}

// FilterUnblocks filters unblock entries based on criteria.
type UnblockFilter struct {
	Domain    string     // Filter by domain (substring match)
	Reason    string     // Filter by reason (exact match)
	StartTime *time.Time // Filter entries after this time
	EndTime   *time.Time // Filter entries before this time
}

// FilterUnblocks returns entries matching the filter criteria.
func FilterUnblocks(entries []UnblockEntry, filter UnblockFilter) []UnblockEntry {
	var result []UnblockEntry

	for _, e := range entries {
		if filter.Domain != "" && !strings.Contains(e.Domain, filter.Domain) {
			continue
		}
		if filter.Reason != "" && e.Reason != filter.Reason {
			continue
		}
		if filter.StartTime != nil && e.UnblockTime.Before(*filter.StartTime) {
			continue
		}
		if filter.EndTime != nil && e.UnblockTime.After(*filter.EndTime) {
			continue
		}
		result = append(result, e)
	}

	return result
}

// ReportFilter filters report entries based on criteria.
type ReportFilter struct {
	Type      ReportType // Filter by report type
	Keyword   string     // Filter by keyword (substring match)
	Domain    string     // Filter by domain (substring match)
	URL       string     // Filter by URL (substring match)
	StartTime *time.Time // Filter entries after this time
	EndTime   *time.Time // Filter entries before this time
}

// FilterReports returns entries matching the filter criteria.
func FilterReports(entries []ReportEntry, filter ReportFilter) []ReportEntry {
	var result []ReportEntry

	for _, e := range entries {
		if filter.Type != "" && e.Type != filter.Type {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(e.Keyword, filter.Keyword) {
			continue
		}
		if filter.Domain != "" && !strings.Contains(e.Domain, filter.Domain) {
			continue
		}
		if filter.URL != "" && !strings.Contains(e.URL, filter.URL) {
			continue
		}
		if filter.StartTime != nil && e.Timestamp.Before(*filter.StartTime) {
			continue
		}
		if filter.EndTime != nil && e.Timestamp.After(*filter.EndTime) {
			continue
		}
		result = append(result, e)
	}

	return result
}

// LifecycleEntry represents a single install/uninstall log entry.
type LifecycleEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`             // "install" or "uninstall"
	Reason    string    `json:"reason,omitempty"` // Only for uninstalls
}

// ParseLifecycleLog reads and parses the lifecycle log file.
func ParseLifecycleLog(path string) ([]LifecycleEntry, error) {
	if path == "" {
		path = DefaultLifecycleLogPath
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []LifecycleEntry
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry LifecycleEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return entries, err
	}

	return entries, nil
}

// LifecycleFilter filters lifecycle entries based on criteria.
type LifecycleFilter struct {
	Type      string     // Filter by type ("install" or "uninstall")
	Reason    string     // Filter by reason (substring match)
	StartTime *time.Time // Filter entries after this time
	EndTime   *time.Time // Filter entries before this time
}

// FilterLifecycle returns entries matching the filter criteria.
func FilterLifecycle(entries []LifecycleEntry, filter LifecycleFilter) []LifecycleEntry {
	var result []LifecycleEntry

	for _, e := range entries {
		if filter.Type != "" && e.Type != filter.Type {
			continue
		}
		if filter.Reason != "" && !strings.Contains(e.Reason, filter.Reason) {
			continue
		}
		if filter.StartTime != nil && e.Timestamp.Before(*filter.StartTime) {
			continue
		}
		if filter.EndTime != nil && e.Timestamp.After(*filter.EndTime) {
			continue
		}
		result = append(result, e)
	}

	return result
}
