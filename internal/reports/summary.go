package reports

import (
	"sort"
	"time"
)

// UnblockSummary provides aggregate statistics for unblock entries.
type UnblockSummary struct {
	TotalCount     int
	ByDomain       map[string]int
	ByReason       map[string]int
	ByDate         map[string]int // date string -> count
	FirstEntry     *time.Time
	LastEntry      *time.Time
}

// SummarizeUnblocks generates summary statistics for unblock entries.
func SummarizeUnblocks(entries []UnblockEntry) UnblockSummary {
	summary := UnblockSummary{
		TotalCount: len(entries),
		ByDomain:   make(map[string]int),
		ByReason:   make(map[string]int),
		ByDate:     make(map[string]int),
	}

	for _, e := range entries {
		summary.ByDomain[e.Domain]++
		summary.ByReason[e.Reason]++
		dateStr := e.UnblockTime.Format("2006-01-02")
		summary.ByDate[dateStr]++

		if summary.FirstEntry == nil || e.UnblockTime.Before(*summary.FirstEntry) {
			t := e.UnblockTime
			summary.FirstEntry = &t
		}
		if summary.LastEntry == nil || e.UnblockTime.After(*summary.LastEntry) {
			t := e.UnblockTime
			summary.LastEntry = &t
		}
	}

	return summary
}

// ReportSummary provides aggregate statistics for report entries.
type ReportSummary struct {
	TotalCount     int
	ByType         map[ReportType]int
	ByKeyword      map[string]int
	ByDomain       map[string]int
	ByDate         map[string]int // date string -> count
	FirstEntry     *time.Time
	LastEntry      *time.Time
}

// SummarizeReports generates summary statistics for report entries.
func SummarizeReports(entries []ReportEntry) ReportSummary {
	summary := ReportSummary{
		TotalCount: len(entries),
		ByType:     make(map[ReportType]int),
		ByKeyword:  make(map[string]int),
		ByDomain:   make(map[string]int),
		ByDate:     make(map[string]int),
	}

	for _, e := range entries {
		summary.ByType[e.Type]++
		summary.ByKeyword[e.Keyword]++
		if e.Domain != "" {
			summary.ByDomain[e.Domain]++
		}
		dateStr := e.Timestamp.Format("2006-01-02")
		summary.ByDate[dateStr]++

		if summary.FirstEntry == nil || e.Timestamp.Before(*summary.FirstEntry) {
			t := e.Timestamp
			summary.FirstEntry = &t
		}
		if summary.LastEntry == nil || e.Timestamp.After(*summary.LastEntry) {
			t := e.Timestamp
			summary.LastEntry = &t
		}
	}

	return summary
}

// CountItem represents a count for a specific item (domain, keyword, etc.)
type CountItem struct {
	Name  string
	Count int
}

// TopN returns the top N items from a count map, sorted by count descending.
func TopN(counts map[string]int, n int) []CountItem {
	items := make([]CountItem, 0, len(counts))
	for name, count := range counts {
		items = append(items, CountItem{Name: name, Count: count})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Count > items[j].Count
	})

	if n > 0 && len(items) > n {
		items = items[:n]
	}

	return items
}

// GroupUnblocksByDay groups unblock entries by day.
func GroupUnblocksByDay(entries []UnblockEntry) map[string][]UnblockEntry {
	result := make(map[string][]UnblockEntry)
	for _, e := range entries {
		dateStr := e.UnblockTime.Format("2006-01-02")
		result[dateStr] = append(result[dateStr], e)
	}
	return result
}

// GroupReportsByDay groups report entries by day.
func GroupReportsByDay(entries []ReportEntry) map[string][]ReportEntry {
	result := make(map[string][]ReportEntry)
	for _, e := range entries {
		dateStr := e.Timestamp.Format("2006-01-02")
		result[dateStr] = append(result[dateStr], e)
	}
	return result
}
