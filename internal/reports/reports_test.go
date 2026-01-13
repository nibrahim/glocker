package reports

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseUnblocksLog(t *testing.T) {
	// Create temp file with test data
	content := `{"unblock_time":"2025-12-05T13:48:24+05:30","restore_time":"2025-12-05T14:18:24+05:30","reason":"work","domain":"youtube.com"}
{"unblock_time":"2025-12-05T22:49:56+05:30","restore_time":"2025-12-05T23:19:56+05:30","reason":"education","domain":"primevideo.com"}
{"unblock_time":"2025-12-08T13:19:54+05:30","restore_time":"2025-12-08T13:39:54+05:30","reason":"work","domain":"youtube.com"}
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "unblocks.log")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseUnblocksLog(tmpFile)
	if err != nil {
		t.Fatalf("ParseUnblocksLog failed: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}

	if entries[0].Domain != "youtube.com" {
		t.Errorf("Expected domain youtube.com, got %s", entries[0].Domain)
	}

	if entries[0].Reason != "work" {
		t.Errorf("Expected reason work, got %s", entries[0].Reason)
	}

	if entries[1].Domain != "primevideo.com" {
		t.Errorf("Expected domain primevideo.com, got %s", entries[1].Domain)
	}
}

func TestParseReportsLog(t *testing.T) {
	content := `[2025-11-17 15:35:46] | url-keyword:porn | https://www.google.com/search?q=test
[2025-11-17 22:51:59] | content-keyword:boobs | https://example.com/page | example.com
[2025-11-17 23:42:33] | content-keyword:pmv | https://pmvhaven.com | pmvhaven.com
`
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "reports.log")
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := ParseReportsLog(tmpFile)
	if err != nil {
		t.Fatalf("ParseReportsLog failed: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}

	if entries[0].Type != ReportTypeURL {
		t.Errorf("Expected type url-keyword, got %s", entries[0].Type)
	}

	if entries[0].Keyword != "porn" {
		t.Errorf("Expected keyword porn, got %s", entries[0].Keyword)
	}

	if entries[1].Type != ReportTypeContent {
		t.Errorf("Expected type content-keyword, got %s", entries[1].Type)
	}

	if entries[1].Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", entries[1].Domain)
	}
}

func TestFilterUnblocks(t *testing.T) {
	now := time.Now()
	entries := []UnblockEntry{
		{UnblockTime: now.Add(-2 * time.Hour), Domain: "youtube.com", Reason: "work"},
		{UnblockTime: now.Add(-1 * time.Hour), Domain: "youtube.com", Reason: "education"},
		{UnblockTime: now, Domain: "instagram.com", Reason: "work"},
	}

	// Filter by domain
	filtered := FilterUnblocks(entries, UnblockFilter{Domain: "youtube"})
	if len(filtered) != 2 {
		t.Errorf("Expected 2 entries for youtube filter, got %d", len(filtered))
	}

	// Filter by reason
	filtered = FilterUnblocks(entries, UnblockFilter{Reason: "work"})
	if len(filtered) != 2 {
		t.Errorf("Expected 2 entries for work reason, got %d", len(filtered))
	}

	// Filter by time
	cutoff := now.Add(-90 * time.Minute)
	filtered = FilterUnblocks(entries, UnblockFilter{StartTime: &cutoff})
	if len(filtered) != 2 {
		t.Errorf("Expected 2 entries after cutoff, got %d", len(filtered))
	}
}

func TestFilterReports(t *testing.T) {
	now := time.Now()
	entries := []ReportEntry{
		{Timestamp: now, Type: ReportTypeURL, Keyword: "porn", Domain: "google.com"},
		{Timestamp: now, Type: ReportTypeContent, Keyword: "xxx", Domain: "example.com"},
		{Timestamp: now, Type: ReportTypeContent, Keyword: "porn", Domain: "search.com"},
	}

	// Filter by type
	filtered := FilterReports(entries, ReportFilter{Type: ReportTypeContent})
	if len(filtered) != 2 {
		t.Errorf("Expected 2 content entries, got %d", len(filtered))
	}

	// Filter by keyword
	filtered = FilterReports(entries, ReportFilter{Keyword: "porn"})
	if len(filtered) != 2 {
		t.Errorf("Expected 2 entries with porn keyword, got %d", len(filtered))
	}

	// Filter by domain
	filtered = FilterReports(entries, ReportFilter{Domain: "google"})
	if len(filtered) != 1 {
		t.Errorf("Expected 1 entry for google domain, got %d", len(filtered))
	}
}

func TestSummarizeUnblocks(t *testing.T) {
	now := time.Now()
	entries := []UnblockEntry{
		{UnblockTime: now, Domain: "youtube.com", Reason: "work"},
		{UnblockTime: now, Domain: "youtube.com", Reason: "education"},
		{UnblockTime: now, Domain: "instagram.com", Reason: "work"},
	}

	summary := SummarizeUnblocks(entries)

	if summary.TotalCount != 3 {
		t.Errorf("Expected total 3, got %d", summary.TotalCount)
	}

	if summary.ByDomain["youtube.com"] != 2 {
		t.Errorf("Expected 2 youtube.com, got %d", summary.ByDomain["youtube.com"])
	}

	if summary.ByReason["work"] != 2 {
		t.Errorf("Expected 2 work, got %d", summary.ByReason["work"])
	}
}

func TestTopN(t *testing.T) {
	counts := map[string]int{
		"a": 10,
		"b": 5,
		"c": 15,
		"d": 3,
	}

	top := TopN(counts, 2)
	if len(top) != 2 {
		t.Errorf("Expected 2 items, got %d", len(top))
	}

	if top[0].Name != "c" || top[0].Count != 15 {
		t.Errorf("Expected c:15 first, got %s:%d", top[0].Name, top[0].Count)
	}

	if top[1].Name != "a" || top[1].Count != 10 {
		t.Errorf("Expected a:10 second, got %s:%d", top[1].Name, top[1].Count)
	}
}
