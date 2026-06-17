// glockpeek - peek at your glocker logs
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"glocker/internal/reports"

	"github.com/tj/go-naturaldate"
)

// ANSI color codes
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[91m"
	colorGreen   = "\033[92m"
	colorYellow  = "\033[93m"
	colorDim     = "\033[2m"
	colorInverse = "\033[7m"
	barChar      = "⣿"
)

func main() {
	unblocksFlag := flag.Bool("unblocks", false, "Show unblocks summary")
	fromDate := flag.String("from", "", "Start date (YYYY, YYYY-MM, or YYYY-MM-DD)")
	toDate := flag.String("to", "", "End date (YYYY, YYYY-MM, or YYYY-MM-DD)")
	detailDate := flag.String("detail", "", "Show detailed logs for a period (date, 'last week', 'last month', etc.)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "glockpeek - peek at your glocker logs\n\n")
		fmt.Fprintf(os.Stderr, "Usage: glockpeek [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  glockpeek                          Show violations summary\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -unblocks                Show unblocks summary\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024               Show all of 2024 onwards\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024-01 -to 2024-06 Show Jan-Jun 2024\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail yesterday        Show detailed day view for yesterday\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail 'last week'      Show detailed day view for each day last week\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail 'last month'     Show detailed month view for last month\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail 2024             Show monthly view for 2024\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail 'last year'      Show monthly view for last year\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail 2024-06          Show detailed logs for June 2024\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -detail 2024-06-15       Show detailed logs for a specific day\n")
	}

	flag.Parse()

	// Handle -detail flag (detailed view with natural date support)
	if *detailDate != "" {
		handleDetail(*detailDate)
		return
	}

	// Parse and validate dates
	var from, to *time.Time
	if *fromDate != "" {
		t, err := parseDateStart(*fromDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid -from date %q\n", *fromDate)
			fmt.Fprintf(os.Stderr, "Supported formats: YYYY, YYYY-MM, YYYY-MM-DD\n")
			os.Exit(1)
		}
		from = &t
	}
	if *toDate != "" {
		t, err := parseDateEnd(*toDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid -to date %q\n", *toDate)
			fmt.Fprintf(os.Stderr, "Supported formats: YYYY, YYYY-MM, YYYY-MM-DD\n")
			os.Exit(1)
		}
		to = &t
	}

	// Validate date range
	if from != nil && to != nil && from.After(*to) {
		fmt.Fprintf(os.Stderr, "Error: -from date must be before -to date\n")
		os.Exit(1)
	}

	if *unblocksFlag {
		printUnblocksSummary(5, from, to)
	} else {
		printViolationsSummary(5, from, to)
	}
}

// handleDetail processes the -detail flag value.
// It supports: YYYY-MM-DD, YYYY-MM, and natural language dates
// like "yesterday", "last week", "last month", "3 days ago".
func handleDetail(input string) {
	// Try exact day format (YYYY-MM-DD)
	if day, err := time.ParseInLocation("2006-01-02", input, time.Local); err == nil {
		printDayDetails(day)
		return
	}
	// Try exact month format (YYYY-MM)
	if month, err := time.ParseInLocation("2006-01", input, time.Local); err == nil {
		printMonthDetails(month)
		return
	}
	// Try exact year format (YYYY)
	if year, err := time.ParseInLocation("2006", input, time.Local); err == nil {
		printYearDetails(year)
		return
	}

	// Try natural language date
	now := time.Now()
	parsed, err := naturaldate.Parse(input, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not parse -detail date %q\n", input)
		fmt.Fprintf(os.Stderr, "Try: YYYY-MM-DD, YYYY-MM, YYYY, 'yesterday', 'last week', 'last month', 'last year', etc.\n")
		os.Exit(1)
	}

	// Determine whether to show a day, week, month, or year view based on the input
	lower := strings.ToLower(input)
	switch {
	case strings.Contains(lower, "year"):
		// Show the year containing the parsed date
		printYearDetails(parsed)
	case strings.Contains(lower, "month"):
		// Show the month containing the parsed date
		printMonthDetails(parsed)
	case strings.Contains(lower, "week"):
		// Show each day of the week containing the parsed date
		printWeekDetails(parsed)
	default:
		// Default to day view
		printDayDetails(parsed)
	}
}

// parseDateStart parses a date string and returns the start of that period.
// Supports: YYYY, YYYY-MM, YYYY-MM-DD
func parseDateStart(s string) (time.Time, error) {
	// Try full date first
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	// Try year-month
	if t, err := time.ParseInLocation("2006-01", s, time.Local); err == nil {
		return t, nil
	}
	// Try year only
	if t, err := time.ParseInLocation("2006", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format")
}

// parseDateEnd parses a date string and returns the end of that period.
// Supports: YYYY, YYYY-MM, YYYY-MM-DD
func parseDateEnd(s string) (time.Time, error) {
	// Try full date first
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		// End of day
		return t.Add(23*time.Hour + 59*time.Minute + 59*time.Second), nil
	}
	// Try year-month
	if t, err := time.ParseInLocation("2006-01", s, time.Local); err == nil {
		// End of month: go to next month, subtract 1 second
		return t.AddDate(0, 1, 0).Add(-time.Second), nil
	}
	// Try year only
	if t, err := time.ParseInLocation("2006", s, time.Local); err == nil {
		// End of year: go to next year, subtract 1 second
		return t.AddDate(1, 0, 0).Add(-time.Second), nil
	}
	return time.Time{}, fmt.Errorf("invalid date format")
}

func printUnblocksSummary(topN int, from, to *time.Time) {
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║              UNBLOCKS SUMMARY                  ║")
	fmt.Println("╚════════════════════════════════════════════════╝")

	entries, err := reports.ParseUnblocksLog("")
	if err != nil {
		fmt.Printf("\nError reading unblocks log: %v\n", err)
		return
	}

	// Apply date filter
	if from != nil || to != nil {
		entries = reports.FilterUnblocks(entries, reports.UnblockFilter{
			StartTime: from,
			EndTime:   to,
		})
	}

	if len(entries) == 0 {
		fmt.Println("\nNo unblock entries found.")
		return
	}

	summary := reports.SummarizeUnblocks(entries)

	fmt.Printf("\nTotal unblocks: %d\n", summary.TotalCount)
	if summary.FirstEntry != nil && summary.LastEntry != nil {
		fmt.Printf("Date range: %s to %s\n",
			summary.FirstEntry.Format("2006-01-02"),
			summary.LastEntry.Format("2006-01-02"))
	}

	// Time of day analysis
	fmt.Println("\n── Time of Day ──")
	hourCounts := make(map[int]int)
	for _, e := range entries {
		hourCounts[e.UnblockTime.Hour()]++
	}
	printHourDistribution(hourCounts)

	// Top domains
	fmt.Printf("\n── Top %d Domains ──\n", topN)
	topDomains := reports.TopN(summary.ByDomain, topN)
	maxLen := maxNameLen(topDomains)
	domainCounts := make([]int, len(topDomains))
	for i, item := range topDomains {
		domainCounts[i] = item.Count
	}
	avgDomains := calcAverage(domainCounts)
	for _, item := range topDomains {
		bar := coloredBar(item.Count, topDomains[0].Count, avgDomains, 20)
		fmt.Printf("  %-*s %3d %s\n", maxLen, item.Name, item.Count, bar)
	}

	// Reasons
	fmt.Printf("\n── Reasons ──\n")
	topReasons := reports.TopN(summary.ByReason, 10)
	maxLen = maxNameLen(topReasons)
	reasonCounts := make([]int, len(topReasons))
	for i, item := range topReasons {
		reasonCounts[i] = item.Count
	}
	avgReasons := calcAverage(reasonCounts)
	for _, item := range topReasons {
		bar := coloredBar(item.Count, topReasons[0].Count, avgReasons, 20)
		fmt.Printf("  %-*s %3d %s\n", maxLen, item.Name, item.Count, bar)
	}

	// Day of week
	fmt.Println("\n── Day of Week ──")
	dayCounts := make(map[string]int)
	for _, e := range entries {
		dayCounts[e.UnblockTime.Weekday().String()]++
	}
	printDayDistribution(dayCounts)
}

func printViolationsSummary(topN int, from, to *time.Time) {
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║             VIOLATIONS SUMMARY                 ║")
	fmt.Println("╚════════════════════════════════════════════════╝")

	entries, err := reports.ParseReportsLog("")
	if err != nil {
		fmt.Printf("\nError reading reports log: %v\n", err)
		return
	}

	// Apply date filter
	if from != nil || to != nil {
		entries = reports.FilterReports(entries, reports.ReportFilter{
			StartTime: from,
			EndTime:   to,
		})
	}

	summary := reports.SummarizeReports(entries)

	// Unmanaged time (glocker uninstalled) counts as a violation of the
	// blocking regime, so surface it in the summary even when there are no
	// content violations to report.
	periods := getUnmanagedPeriods()
	rangeStart, rangeEnd := summaryUnmanagedRange(from, to, summary, periods)

	if len(entries) == 0 {
		fmt.Println("\nNo violation entries found.")
		if !rangeStart.IsZero() {
			printUnmanagedSummary(rangeStart, rangeEnd, periods)
		}
		return
	}

	fmt.Printf("\nTotal violations: %d\n", summary.TotalCount)
	if summary.FirstEntry != nil && summary.LastEntry != nil {
		fmt.Printf("Date range: %s to %s\n",
			summary.FirstEntry.Format("2006-01-02"),
			summary.LastEntry.Format("2006-01-02"))
	}

	// Unmanaged time section, marked as a violation.
	printUnmanagedSummary(rangeStart, rangeEnd, periods)

	// By type
	fmt.Println("\n── By Type ──")
	fmt.Printf("  URL keyword:     %d\n", summary.ByType[reports.ReportTypeURL])
	fmt.Printf("  Content keyword: %d\n", summary.ByType[reports.ReportTypeContent])

	// Time of day analysis with top keyword per period
	fmt.Println("\n── Time of Day ──")
	printViolationsHourDistribution(entries)

	// Top keywords with most common time period
	fmt.Printf("\n── Top %d Keywords ──\n", topN)
	keywordPeriods := buildKeywordPeriodMap(entries)
	topKeywords := reports.TopN(summary.ByKeyword, topN)
	maxLen := maxNameLen(topKeywords)
	keywordCounts := make([]int, len(topKeywords))
	for i, item := range topKeywords {
		keywordCounts[i] = item.Count
	}
	avgKeywords := calcAverage(keywordCounts)
	for _, item := range topKeywords {
		bar := coloredBar(item.Count, topKeywords[0].Count, avgKeywords, 20)
		period := getTopPeriodForKeyword(keywordPeriods, item.Name)
		fmt.Printf("  %-*s %3d %s (%s)\n", maxLen, item.Name, item.Count, bar, period)
	}

	// Top domains
	fmt.Printf("\n── Top %d Domains ──\n", topN)
	topDomains := reports.TopN(summary.ByDomain, topN)
	maxLen = maxNameLen(topDomains)
	domainCounts := make([]int, len(topDomains))
	for i, item := range topDomains {
		domainCounts[i] = item.Count
	}
	avgDomains := calcAverage(domainCounts)
	for _, item := range topDomains {
		bar := coloredBar(item.Count, topDomains[0].Count, avgDomains, 20)
		fmt.Printf("  %-*s %3d %s\n", maxLen, item.Name, item.Count, bar)
	}

	// Day of week
	fmt.Println("\n── Day of Week ──")
	dayCounts := make(map[string]int)
	for _, e := range entries {
		dayCounts[e.Timestamp.Weekday().String()]++
	}
	printDayDistribution(dayCounts)
}

func printHourDistribution(hourCounts map[int]int) {
	maxCount := 0
	for _, c := range hourCounts {
		if c > maxCount {
			maxCount = c
		}
	}

	// Group into time periods
	periods := map[string]int{
		"Night (00-06)":     0,
		"Morning (06-12)":   0,
		"Afternoon (12-18)": 0,
		"Evening (18-24)":   0,
	}

	for hour, count := range hourCounts {
		switch {
		case hour < 6:
			periods["Night (00-06)"] += count
		case hour < 12:
			periods["Morning (06-12)"] += count
		case hour < 18:
			periods["Afternoon (12-18)"] += count
		default:
			periods["Evening (18-24)"] += count
		}
	}

	// Find max and calculate average for scaling
	maxPeriod := 0
	periodCounts := make([]int, 0, 4)
	for _, c := range periods {
		if c > maxPeriod {
			maxPeriod = c
		}
		periodCounts = append(periodCounts, c)
	}
	avgPeriod := calcAverage(periodCounts)

	order := []string{"Night (00-06)", "Morning (06-12)", "Afternoon (12-18)", "Evening (18-24)"}
	for _, name := range order {
		count := periods[name]
		bar := coloredBar(count, maxPeriod, avgPeriod, 20)
		fmt.Printf("  %-18s %3d %s\n", name, count, bar)
	}
}

func printViolationsHourDistribution(entries []reports.ReportEntry) {
	// Track counts and keywords per period
	type periodData struct {
		count    int
		keywords map[string]int
	}

	periods := map[string]*periodData{
		"Night (00-06)":     {keywords: make(map[string]int)},
		"Morning (06-12)":   {keywords: make(map[string]int)},
		"Afternoon (12-18)": {keywords: make(map[string]int)},
		"Evening (18-24)":   {keywords: make(map[string]int)},
	}

	for _, e := range entries {
		hour := e.Timestamp.Hour()
		var period string
		switch {
		case hour < 6:
			period = "Night (00-06)"
		case hour < 12:
			period = "Morning (06-12)"
		case hour < 18:
			period = "Afternoon (12-18)"
		default:
			period = "Evening (18-24)"
		}
		periods[period].count++
		periods[period].keywords[e.Keyword]++
	}

	// Find max and calculate average for scaling
	maxPeriod := 0
	periodCounts := make([]int, 0, 4)
	for _, p := range periods {
		if p.count > maxPeriod {
			maxPeriod = p.count
		}
		periodCounts = append(periodCounts, p.count)
	}
	avgPeriod := calcAverage(periodCounts)

	// Find top keyword per period
	topKeyword := func(keywords map[string]int) string {
		top := ""
		topCount := 0
		for k, c := range keywords {
			if c > topCount {
				top = k
				topCount = c
			}
		}
		return top
	}

	order := []string{"Night (00-06)", "Morning (06-12)", "Afternoon (12-18)", "Evening (18-24)"}
	for _, name := range order {
		p := periods[name]
		bar := coloredBar(p.count, maxPeriod, avgPeriod, 20)
		keyword := topKeyword(p.keywords)
		if keyword != "" {
			fmt.Printf("  %-18s %3d %s (%s)\n", name, p.count, bar, keyword)
		} else {
			fmt.Printf("  %-18s %3d %s\n", name, p.count, bar)
		}
	}
}

func printDayDistribution(dayCounts map[string]int) {
	days := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

	maxCount := 0
	dayCntSlice := make([]int, 0, 7)
	for _, day := range days {
		c := dayCounts[day]
		if c > maxCount {
			maxCount = c
		}
		dayCntSlice = append(dayCntSlice, c)
	}
	avgCount := calcAverage(dayCntSlice)

	for _, day := range days {
		count := dayCounts[day]
		bar := coloredBar(count, maxCount, avgCount, 20)
		fmt.Printf("  %-9s %3d %s\n", day, count, bar)
	}
}

func barLen(count, maxCount, maxLen int) int {
	if maxCount == 0 {
		return 0
	}
	return (count * maxLen) / maxCount
}

// quantizedBarLen returns a quantized bar length:
// 0 → 0, 1-3 → 1, 4-7 → 2, 8-11 → 3, etc.
func quantizedBarLen(count, maxLen int) int {
	if count == 0 {
		return 0
	}
	if count <= 3 {
		return 1
	}
	bars := (count-4)/4 + 2
	if bars > maxLen {
		return maxLen
	}
	return bars
}

// coloredBar returns a colored bar string based on whether count is above average
func coloredBar(count, maxCount, avg, maxLen int) string {
	length := barLen(count, maxCount, maxLen)
	bar := strings.Repeat(barChar, length)

	if count > avg {
		return colorRed + bar + colorReset
	}
	return colorGreen + bar + colorReset
}

// coloredBarAbsolute returns a colored bar with absolute thresholds:
// - 2 or less: green (accidental)
// - More than 2: red (deliberate attempt)
// Bar length is quantized: 0→0, 1-3→1, 4-7→2, 8-11→3, etc.
func coloredBarAbsolute(count, maxLen int) string {
	length := quantizedBarLen(count, maxLen)
	bar := strings.Repeat(barChar, length)

	if count > 2 {
		return colorRed + bar + colorReset
	}
	return colorGreen + bar + colorReset
}

// calcAverage calculates the average of counts
func calcAverage(counts []int) int {
	if len(counts) == 0 {
		return 0
	}
	sum := 0
	for _, c := range counts {
		sum += c
	}
	return sum / len(counts)
}

func maxNameLen(items []reports.CountItem) int {
	maxLen := 0
	for _, item := range items {
		if len(item.Name) > maxLen {
			maxLen = len(item.Name)
		}
	}
	return maxLen
}

// buildKeywordPeriodMap builds a map of keyword -> period -> count
func buildKeywordPeriodMap(entries []reports.ReportEntry) map[string]map[string]int {
	result := make(map[string]map[string]int)

	for _, e := range entries {
		if result[e.Keyword] == nil {
			result[e.Keyword] = make(map[string]int)
		}

		hour := e.Timestamp.Hour()
		var period string
		switch {
		case hour < 6:
			period = "night"
		case hour < 12:
			period = "morning"
		case hour < 18:
			period = "afternoon"
		default:
			period = "evening"
		}
		result[e.Keyword][period]++
	}

	return result
}

// getTopPeriodForKeyword returns the most common time period for a keyword
func getTopPeriodForKeyword(keywordPeriods map[string]map[string]int, keyword string) string {
	periods := keywordPeriods[keyword]
	if periods == nil {
		return "unknown"
	}

	topPeriod := ""
	topCount := 0
	for period, count := range periods {
		if count > topCount {
			topPeriod = period
			topCount = count
		}
	}

	return topPeriod
}

// hourlyStats holds aggregated data for one hour
type hourlyStats struct {
	violations int
	keywords   map[string]int
	domains    map[string]int
}

// unmanagedPeriod represents a time range when glocker was not running
type unmanagedPeriod struct {
	start  time.Time
	end    time.Time // zero time means still unmanaged
	reason string
	note   string
}

// minUnmanagedDuration is the minimum duration to consider as a real unmanaged period.
// Shorter periods (e.g., during upgrades) are ignored.
const minUnmanagedDuration = 2 * time.Minute

// getUnmanagedPeriods parses the lifecycle log and returns periods when glocker was uninstalled.
// Short periods (< 2 minutes) are filtered out as they're likely just upgrades.
func getUnmanagedPeriods() []unmanagedPeriod {
	entries, err := reports.ParseLifecycleLog("")
	if err != nil {
		return nil
	}

	var periods []unmanagedPeriod
	var currentUninstall *time.Time
	var currentReason string
	var currentNote string

	for _, e := range entries {
		if e.Type == "uninstall" {
			currentUninstall = &e.Timestamp
			currentReason = e.Reason
			currentNote = e.Note
		} else if e.Type == "install" && currentUninstall != nil {
			duration := e.Timestamp.Sub(*currentUninstall)
			// Only include periods longer than the minimum (skip quick upgrades)
			if duration >= minUnmanagedDuration {
				periods = append(periods, unmanagedPeriod{
					start:  *currentUninstall,
					end:    e.Timestamp,
					reason: currentReason,
					note:   currentNote,
				})
			}
			currentUninstall = nil
			currentReason = ""
			currentNote = ""
		}
	}

	// If currently unmanaged (uninstall without subsequent install)
	if currentUninstall != nil {
		periods = append(periods, unmanagedPeriod{
			start:  *currentUninstall,
			end:    time.Time{}, // zero time = still unmanaged
			reason: currentReason,
			note:   currentNote,
		})
	}

	return periods
}

// unmanagedReasonsForRange returns unique reason labels from unmanaged periods
// overlapping [start, end). When a period has a note, it is appended after the
// reason as "reason — note".
func unmanagedReasonsForRange(start, end time.Time, periods []unmanagedPeriod) []string {
	seen := make(map[string]bool)
	var reasons []string
	for _, p := range periods {
		pEnd := p.end
		if pEnd.IsZero() {
			pEnd = time.Now()
		}
		if !(start.Before(pEnd) && end.After(p.start)) || p.reason == "" {
			continue
		}
		label := p.reason
		if p.note != "" {
			label = p.reason + " — " + p.note
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		reasons = append(reasons, label)
	}
	return reasons
}

// isHourUnmanaged checks if a specific hour on a day overlaps with any unmanaged period
func isHourUnmanaged(day time.Time, hour int, periods []unmanagedPeriod) bool {
	hourStart := time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, time.Local)
	hourEnd := hourStart.Add(time.Hour)

	for _, p := range periods {
		pEnd := p.end
		if pEnd.IsZero() {
			pEnd = time.Now()
		}
		// Check if hour overlaps with unmanaged period
		if hourStart.Before(pEnd) && hourEnd.After(p.start) {
			return true
		}
	}
	return false
}

// isDayUnmanaged checks if any part of a day overlaps with any unmanaged period
func isDayUnmanaged(day time.Time, periods []unmanagedPeriod) bool {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)

	for _, p := range periods {
		pEnd := p.end
		if pEnd.IsZero() {
			pEnd = time.Now()
		}
		// Check if day overlaps with unmanaged period
		if dayStart.Before(pEnd) && dayEnd.After(p.start) {
			return true
		}
	}
	return false
}

// getUnmanagedHoursInDay returns the number of unmanaged hours in a day
func getUnmanagedHoursInDay(day time.Time, periods []unmanagedPeriod) int {
	count := 0
	for hour := 0; hour < 24; hour++ {
		if isHourUnmanaged(day, hour, periods) {
			count++
		}
	}
	return count
}

// printDayDetails prints aggregated hourly logs for a specific day
func printDayDetails(day time.Time) {
	dayStart := day
	dayEnd := day.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  DAILY LOG: %-34s ║\n", day.Format("Monday, January 2, 2006"))
	fmt.Printf("╚════════════════════════════════════════════════╝\n")

	// Get unmanaged periods
	unmanagedPeriods := getUnmanagedPeriods()

	// Get violations for this day
	violations, _ := reports.ParseReportsLog("")
	violations = reports.FilterReports(violations, reports.ReportFilter{
		StartTime: &dayStart,
		EndTime:   &dayEnd,
	})

	// Get lifecycle events for this day
	lifecycle, _ := reports.ParseLifecycleLog("")
	lifecycle = reports.FilterLifecycle(lifecycle, reports.LifecycleFilter{
		StartTime: &dayStart,
		EndTime:   &dayEnd,
	})

	// Initialize hourly stats for all 24 hours
	hourlyData := make(map[int]*hourlyStats)
	for h := 0; h < 24; h++ {
		hourlyData[h] = &hourlyStats{
			keywords: make(map[string]int),
			domains:  make(map[string]int),
		}
	}

	// Aggregate violations by hour
	for _, v := range violations {
		h := v.Timestamp.Hour()
		hourlyData[h].violations++
		hourlyData[h].keywords[v.Keyword]++
		hourlyData[h].domains[v.Domain]++
	}

	// Determine last hour to show (current hour if today, else 23)
	now := time.Now()
	lastHour := 23
	if day.Year() == now.Year() && day.YearDay() == now.YearDay() {
		lastHour = now.Hour()
	}

	// Print hourly aggregation
	fmt.Printf("\n%d violations:\n\n", len(violations))
	cleanHours := 0
	unmanagedHours := 0
	for hour := 0; hour <= lastHour; hour++ {
		stats := hourlyData[hour]
		hourLabel := fmt.Sprintf("%02d:00", hour)
		isUnmanaged := isHourUnmanaged(day, hour, unmanagedPeriods)

		if isUnmanaged {
			// Unmanaged hour - show red block with reason
			hourStart := time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, time.Local)
			hourEnd := hourStart.Add(time.Hour)
			reasons := unmanagedReasonsForRange(hourStart, hourEnd, unmanagedPeriods)
			reasonStr := ""
			if len(reasons) > 0 {
				reasonStr = fmt.Sprintf(" %s(%s)%s", colorDim, strings.Join(reasons, ", "), colorReset)
			}
			fmt.Printf("── %s%s%s %s████ UNMANAGED%s%s\n", colorRed, hourLabel, colorReset, colorRed, colorReset, reasonStr)
			unmanagedHours++
		} else if stats.violations == 0 {
			// Clean hour - show dim indicator
			fmt.Printf("── %s%s%s %s·%s\n", colorDim, hourLabel, colorReset, colorDim, colorReset)
			cleanHours++
		} else {
			// Hour with violations - show aggregated data
			isEgregious := stats.violations > 2

			// Build the hour header
			if isEgregious {
				fmt.Printf("── %s%s%s", colorInverse, hourLabel, colorReset)
			} else {
				fmt.Printf("── %s", hourLabel)
			}

			// Add bar visualization with absolute thresholds
			bar := coloredBarAbsolute(stats.violations, 15)
			fmt.Printf(" %s", bar)

			// Add count
			fmt.Printf(" V:%d", stats.violations)

			// Add context (top keyword)
			if topKw := getTopKey(stats.keywords); topKw != "" {
				fmt.Printf(" %s(%s)%s", colorDim, topKw, colorReset)
			}

			fmt.Println()
		}
	}

	// Print lifecycle events
	if len(lifecycle) > 0 {
		fmt.Println("\n── Lifecycle Events ──")
		for _, e := range lifecycle {
			color := colorGreen
			if e.Type == "uninstall" {
				color = colorRed
			}
			detail := e.Reason
			if e.Note != "" {
				if detail != "" {
					detail += " — " + e.Note
				} else {
					detail = e.Note
				}
			}
			if detail != "" {
				fmt.Printf("  %s%s%s %s%s%s (%s)\n",
					colorDim, e.Timestamp.Format("15:04"), colorReset,
					color, e.Type, colorReset,
					detail)
			} else {
				fmt.Printf("  %s%s%s %s%s%s\n",
					colorDim, e.Timestamp.Format("15:04"), colorReset,
					color, e.Type, colorReset)
			}
		}
	}

	// Print summary for the day
	fmt.Println("\n── Day Summary ──")
	fmt.Printf("  Violations: %d\n", len(violations))
	if unmanagedHours > 0 {
		unmanagedMin := calculateUnmanagedMinutesForDay(day)
		fmt.Printf("  Hours:      %d (%s%d clean%s, %s%d unmanaged%s)\n",
			lastHour+1, colorGreen, cleanHours, colorReset, colorRed, unmanagedHours, colorReset)
		fmt.Printf("  Unmanaged:  %s%d minutes%s\n", colorRed, unmanagedMin, colorReset)
	} else {
		fmt.Printf("  Hours:      %d (%s%d clean%s)\n", lastHour+1, colorGreen, cleanHours, colorReset)
	}

	// Top keywords across the day
	dayKeywords := make(map[string]int)
	for _, v := range violations {
		dayKeywords[v.Keyword]++
	}
	if len(dayKeywords) > 0 {
		topKw := getTopFromMap(dayKeywords, 3)
		fmt.Print("  Top keywords: ")
		fmt.Println(strings.Join(topKw, ", "))
	}
}

// getTopKey returns the key with highest count from a map
func getTopKey(m map[string]int) string {
	topKey := ""
	topCount := 0
	for k, c := range m {
		if c > topCount {
			topKey = k
			topCount = c
		}
	}
	return topKey
}

// printMonthDetails prints aggregated daily logs for a specific month
func printMonthDetails(month time.Time) {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Second)

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  MONTHLY LOG: %-32s ║\n", month.Format("January 2006"))
	fmt.Printf("╚════════════════════════════════════════════════╝\n")

	// Get unmanaged periods
	unmanagedPeriods := getUnmanagedPeriods()

	// Get violations for this month
	violations, _ := reports.ParseReportsLog("")
	violations = reports.FilterReports(violations, reports.ReportFilter{
		StartTime: &monthStart,
		EndTime:   &monthEnd,
	})

	// Aggregate by day
	type dayStats struct {
		date            time.Time
		violations      int
		topKeyword      string
		topKeywordCount int
		topPeriod       string
		topPeriodCount  int
		keywords        map[string]int
		periods         map[string]int
		isUnmanaged     bool
		unmanagedHours  int
	}

	days := make(map[string]*dayStats)

	// Initialize all days of the month (up to today if current month)
	now := time.Now()
	lastDay := monthEnd
	if month.Year() == now.Year() && month.Month() == now.Month() {
		// For current month, only show up to today
		lastDay = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.Local)
	}
	for d := monthStart; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dayStr := d.Format("2006-01-02")
		days[dayStr] = &dayStats{
			date:           d,
			keywords:       make(map[string]int),
			periods:        make(map[string]int),
			isUnmanaged:    isDayUnmanaged(d, unmanagedPeriods),
			unmanagedHours: getUnmanagedHoursInDay(d, unmanagedPeriods),
		}
	}

	// Process violations
	for _, v := range violations {
		dayStr := v.Timestamp.Format("2006-01-02")
		if days[dayStr] == nil {
			days[dayStr] = &dayStats{
				date:     v.Timestamp,
				keywords: make(map[string]int),
				periods:  make(map[string]int),
			}
		}
		d := days[dayStr]
		d.violations++
		d.keywords[v.Keyword]++
		d.periods[getTimePeriod(v.Timestamp.Hour())]++
	}

	// Calculate top items for each day
	for _, d := range days {
		for k, c := range d.keywords {
			if c > d.topKeywordCount {
				d.topKeyword = k
				d.topKeywordCount = c
			}
		}
		for p, c := range d.periods {
			if c > d.topPeriodCount {
				d.topPeriod = p
				d.topPeriodCount = c
			}
		}
	}

	// Sort days
	var sortedDays []string
	for day := range days {
		sortedDays = append(sortedDays, day)
	}
	for i := 0; i < len(sortedDays)-1; i++ {
		for j := i + 1; j < len(sortedDays); j++ {
			if sortedDays[j] < sortedDays[i] {
				sortedDays[i], sortedDays[j] = sortedDays[j], sortedDays[i]
			}
		}
	}

	// Calculate totals
	totalV := 0
	daysWithViolations := 0
	unmanagedDays := 0
	for _, dayStr := range sortedDays {
		d := days[dayStr]
		totalV += d.violations
		if d.violations > 0 {
			daysWithViolations++
		}
		if d.isUnmanaged {
			unmanagedDays++
		}
	}

	// Print compact daily summary
	fmt.Println()
	for _, dayStr := range sortedDays {
		d := days[dayStr]

		// Format: "Jan 02 Mon │ ████ V:12 (afternoon, porn)"
		datePart := fmt.Sprintf("%s %s", d.date.Format("Jan 02"), d.date.Format("Mon")[:3])

		// Get unmanaged reasons for this day
		dayStart := time.Date(d.date.Year(), d.date.Month(), d.date.Day(), 0, 0, 0, 0, time.Local)
		dayEnd := dayStart.Add(24 * time.Hour)
		reasons := unmanagedReasonsForRange(dayStart, dayEnd, unmanagedPeriods)
		reasonStr := ""
		if len(reasons) > 0 {
			reasonStr = fmt.Sprintf(" %s(%s)%s", colorDim, strings.Join(reasons, ", "), colorReset)
		}

		// Check for unmanaged day first
		if d.isUnmanaged && d.unmanagedHours == 24 {
			// Fully unmanaged day
			datePart = fmt.Sprintf("%s%s%s", colorRed, datePart, colorReset)
			line := datePart + " │"
			line += fmt.Sprintf(" %s████ UNMANAGED%s%s", colorRed, colorReset, reasonStr)
			fmt.Println(line)
			continue
		}

		// Highlight deliberate days (more than 2 violations)
		isEgregious := d.violations > 2
		if isEgregious {
			datePart = fmt.Sprintf("%s%s%s", colorInverse, datePart, colorReset)
		}
		line := datePart + " │"

		if d.isUnmanaged {
			// Partially unmanaged day - show hours
			line += fmt.Sprintf(" %s██%s %dh unmanaged", colorRed, colorReset, d.unmanagedHours)
			if d.violations > 0 {
				line += fmt.Sprintf(" V:%d", d.violations)
			}
			line += reasonStr
		} else if d.violations > 0 {
			// Add bar with absolute thresholds
			bar := coloredBarAbsolute(d.violations, 10)
			line += fmt.Sprintf(" %s", bar)

			line += fmt.Sprintf(" V:%d", d.violations)
			if d.topPeriod != "" || d.topKeyword != "" {
				parts := []string{}
				if d.topPeriod != "" {
					parts = append(parts, d.topPeriod)
				}
				if d.topKeyword != "" {
					parts = append(parts, d.topKeyword)
				}
				line += fmt.Sprintf(" %s(%s)%s", colorDim, strings.Join(parts, ", "), colorReset)
			}
		} else {
			// Show clean indicator for days with no violations
			line += fmt.Sprintf(" %s·%s", colorDim, colorReset)
		}

		fmt.Println(line)
	}

	// Month totals
	cleanDays := len(days) - daysWithViolations - unmanagedDays
	if unmanagedDays > 0 {
		fmt.Printf("\n── Totals: %sV:%d%s │ %d days (%s%d clean%s, %s%d unmanaged%s) ──\n",
			colorRed, totalV, colorReset,
			len(days),
			colorGreen, cleanDays, colorReset,
			colorRed, unmanagedDays, colorReset)
	} else {
		fmt.Printf("\n── Totals: %sV:%d%s │ %d days (%s%d clean%s) ──\n",
			colorRed, totalV, colorReset,
			len(days),
			colorGreen, cleanDays, colorReset)
	}
}

// printYearDetails prints aggregated monthly logs for a specific year.
func printYearDetails(year time.Time) {
	yearStart := time.Date(year.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	yearEnd := yearStart.AddDate(1, 0, 0).Add(-time.Second)

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  YEARLY LOG: %-33s ║\n", year.Format("2006"))
	fmt.Printf("╚════════════════════════════════════════════════╝\n")

	// Get unmanaged periods
	unmanagedPeriods := getUnmanagedPeriods()

	// Get violations for this year
	violations, _ := reports.ParseReportsLog("")
	violations = reports.FilterReports(violations, reports.ReportFilter{
		StartTime: &yearStart,
		EndTime:   &yearEnd,
	})

	// Aggregate by month
	type monthStats struct {
		month           time.Time
		violations      int
		topKeyword      string
		topKeywordCount int
		keywords        map[string]int
		unmanagedDays   int
		unmanagedReasons []string
	}

	now := time.Now()
	lastMonth := 12
	if year.Year() == now.Year() {
		lastMonth = int(now.Month())
	}

	months := make([]*monthStats, lastMonth)
	for i := 0; i < lastMonth; i++ {
		m := time.Date(year.Year(), time.Month(i+1), 1, 0, 0, 0, 0, time.Local)
		ms := &monthStats{
			month:    m,
			keywords: make(map[string]int),
		}

		// Count unmanaged days and collect reasons for this month
		mEnd := m.AddDate(0, 1, 0)
		daysInMonth := int(mEnd.Sub(m).Hours() / 24)
		for d := 0; d < daysInMonth; d++ {
			day := m.AddDate(0, 0, d)
			if day.After(now) {
				break
			}
			if isDayUnmanaged(day, unmanagedPeriods) {
				ms.unmanagedDays++
			}
		}

		ms.unmanagedReasons = unmanagedReasonsForRange(m, mEnd, unmanagedPeriods)

		months[i] = ms
	}

	// Process violations
	for _, v := range violations {
		mIdx := int(v.Timestamp.Month()) - 1
		if mIdx < len(months) {
			months[mIdx].violations++
			months[mIdx].keywords[v.Keyword]++
		}
	}

	// Calculate top keyword per month
	for _, ms := range months {
		for k, c := range ms.keywords {
			if c > ms.topKeywordCount {
				ms.topKeyword = k
				ms.topKeywordCount = c
			}
		}
	}

	// Print monthly summary
	fmt.Println()
	totalV := 0
	cleanMonths := 0
	unmanagedMonths := 0

	for _, ms := range months {
		totalV += ms.violations
		datePart := ms.month.Format("January")

		// Check for fully unmanaged month
		mEnd := ms.month.AddDate(0, 1, 0)
		daysInMonth := int(mEnd.Sub(ms.month).Hours() / 24)
		padded := fmt.Sprintf("%-9s", datePart)
		reasonStr := ""
		if len(ms.unmanagedReasons) > 0 {
			reasonStr = fmt.Sprintf(" %s(%s)%s", colorDim, strings.Join(ms.unmanagedReasons, ", "), colorReset)
		}

		if ms.unmanagedDays == daysInMonth {
			fmt.Printf("  %s%s%s │ %s████ UNMANAGED%s%s\n", colorRed, padded, colorReset, colorRed, colorReset, reasonStr)
			unmanagedMonths++
			continue
		}

		isEgregious := ms.violations > 2
		if isEgregious {
			padded = fmt.Sprintf("%s%s%s", colorInverse, padded, colorReset)
		}
		line := fmt.Sprintf("  %s │", padded)

		if ms.unmanagedDays > 0 {
			line += fmt.Sprintf(" %s██%s %dd unmanaged", colorRed, colorReset, ms.unmanagedDays)
			if ms.violations > 0 {
				line += fmt.Sprintf(" V:%d", ms.violations)
			}
			line += reasonStr
			unmanagedMonths++
		} else if ms.violations > 0 {
			bar := coloredBarAbsolute(ms.violations, 10)
			line += fmt.Sprintf(" %s V:%d", bar, ms.violations)
			if ms.topKeyword != "" {
				line += fmt.Sprintf(" %s(%s)%s", colorDim, ms.topKeyword, colorReset)
			}
		} else {
			line += fmt.Sprintf(" %s·%s", colorDim, colorReset)
			cleanMonths++
		}

		fmt.Println(line)
	}

	// Year totals
	fmt.Printf("\n── Totals: %sV:%d%s │ %d months (%s%d clean%s",
		colorRed, totalV, colorReset,
		lastMonth,
		colorGreen, cleanMonths, colorReset)
	if unmanagedMonths > 0 {
		fmt.Printf(", %s%d unmanaged%s", colorRed, unmanagedMonths, colorReset)
	}
	fmt.Println(") ──")
}

// printWeekDetails prints day-by-day details for the week containing the given date.
// The week runs Monday through Sunday.
func printWeekDetails(ref time.Time) {
	// Find Monday of the week containing ref
	weekday := ref.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := time.Date(ref.Year(), ref.Month(), ref.Day()-int(weekday-time.Monday), 0, 0, 0, 0, time.Local)
	sunday := monday.AddDate(0, 0, 6)

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  WEEKLY LOG: %s to %s  ║\n",
		monday.Format("Jan 02"), sunday.Format("Jan 02, 2006"))
	fmt.Printf("╚════════════════════════════════════════════════╝\n")

	// Get unmanaged periods
	unmanagedPeriods := getUnmanagedPeriods()

	// Get violations for the whole week
	weekStart := monday
	weekEnd := sunday.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	violations, _ := reports.ParseReportsLog("")
	violations = reports.FilterReports(violations, reports.ReportFilter{
		StartTime: &weekStart,
		EndTime:   &weekEnd,
	})

	// Group violations by day
	dayViolations := make(map[string][]reports.ReportEntry)
	for _, v := range violations {
		dayStr := v.Timestamp.Format("2006-01-02")
		dayViolations[dayStr] = append(dayViolations[dayStr], v)
	}

	now := time.Now()
	totalV := 0
	cleanDays := 0
	unmanagedDays := 0

	fmt.Println()
	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		if day.After(now) {
			break
		}
		dayStr := day.Format("2006-01-02")
		vForDay := dayViolations[dayStr]
		totalV += len(vForDay)

		datePart := fmt.Sprintf("%s %s", day.Format("Jan 02"), day.Format("Mon")[:3])
		isUnmanaged := isDayUnmanaged(day, unmanagedPeriods)

		// Get unmanaged reasons for this day
		dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
		dayEnd := dayStart.Add(24 * time.Hour)
		reasons := unmanagedReasonsForRange(dayStart, dayEnd, unmanagedPeriods)
		reasonStr := ""
		if len(reasons) > 0 {
			reasonStr = fmt.Sprintf(" %s(%s)%s", colorDim, strings.Join(reasons, ", "), colorReset)
		}

		if isUnmanaged && getUnmanagedHoursInDay(day, unmanagedPeriods) == 24 {
			datePart = fmt.Sprintf("%s%s%s", colorRed, datePart, colorReset)
			fmt.Printf("%s │ %s████ UNMANAGED%s%s\n", datePart, colorRed, colorReset, reasonStr)
			unmanagedDays++
			continue
		}

		if len(vForDay) > 2 {
			datePart = fmt.Sprintf("%s%s%s", colorInverse, datePart, colorReset)
		}

		if isUnmanaged {
			unmanagedHours := getUnmanagedHoursInDay(day, unmanagedPeriods)
			fmt.Printf("%s │ %s██%s %dh unmanaged", datePart, colorRed, colorReset, unmanagedHours)
			if len(vForDay) > 0 {
				fmt.Printf(" V:%d", len(vForDay))
			}
			fmt.Printf("%s\n", reasonStr)
			unmanagedDays++
		} else if len(vForDay) == 0 {
			fmt.Printf("%s │ %s·%s\n", datePart, colorDim, colorReset)
			cleanDays++
		} else {
			bar := coloredBarAbsolute(len(vForDay), 10)
			keywords := make(map[string]int)
			for _, v := range vForDay {
				keywords[v.Keyword]++
			}
			topKw := getTopFromMap(keywords, 1)
			kwStr := ""
			if len(topKw) > 0 {
				kwStr = fmt.Sprintf(" %s(%s)%s", colorDim, topKw[0], colorReset)
			}
			fmt.Printf("%s │ %s V:%d%s\n", datePart, bar, len(vForDay), kwStr)
		}
	}

	fmt.Printf("\n── Totals: %sV:%d%s │ %s%d clean%s",
		colorRed, totalV, colorReset,
		colorGreen, cleanDays, colorReset)
	if unmanagedDays > 0 {
		fmt.Printf(", %s%d unmanaged%s", colorRed, unmanagedDays, colorReset)
	}
	fmt.Println(" ──")
}

// getTimePeriod returns the time period name for an hour
func getTimePeriod(hour int) string {
	switch {
	case hour < 6:
		return "night"
	case hour < 12:
		return "morning"
	case hour < 18:
		return "afternoon"
	default:
		return "evening"
	}
}

// getTopFromMap returns top N keys from a map sorted by value
func getTopFromMap(m map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var sorted []kv
	for k, v := range m {
		sorted = append(sorted, kv{k, v})
	}
	// Simple bubble sort
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].v > sorted[i].v {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	var result []string
	for i := 0; i < n && i < len(sorted); i++ {
		result = append(result, sorted[i].k)
	}
	return result
}


// calculateUnmanagedMinutesForDay calculates total unmanaged minutes for a day.
func calculateUnmanagedMinutesForDay(date time.Time) int {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	dayEnd := dayStart.Add(24 * time.Hour)
	return calculateUnmanagedMinutesInRange(dayStart, dayEnd, getUnmanagedPeriods())
}

// calculateUnmanagedMinutesInRange returns the total minutes glocker was
// unmanaged that overlap [start, end).
func calculateUnmanagedMinutesInRange(start, end time.Time, periods []unmanagedPeriod) int {
	total := 0
	for _, p := range periods {
		pStart := p.start
		pEnd := p.end
		if pEnd.IsZero() {
			pEnd = time.Now()
		}
		// Clamp to the requested range.
		if pStart.Before(start) {
			pStart = start
		}
		if pEnd.After(end) {
			pEnd = end
		}
		if pStart.Before(pEnd) {
			total += int(pEnd.Sub(pStart).Minutes())
		}
	}
	return total
}

// formatUnmanagedDuration renders a minute count as "45m", "2h 15m", or
// "3d 4h 5m" for longer spans.
func formatUnmanagedDuration(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	days := minutes / (24 * 60)
	hours := (minutes % (24 * 60)) / 60
	mins := minutes % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// summaryUnmanagedRange resolves the [start, end) window over which the
// violations summary reports unmanaged time. When no -from/-to is given, it
// spans from the earliest known activity (first violation or first unmanaged
// period) to now. Returns a zero start when nothing is known.
func summaryUnmanagedRange(from, to *time.Time, summary reports.ReportSummary, periods []unmanagedPeriod) (time.Time, time.Time) {
	end := time.Now()
	if to != nil {
		end = *to
	}
	if from != nil {
		return *from, end
	}

	start := time.Time{}
	if summary.FirstEntry != nil {
		start = *summary.FirstEntry
	}
	for _, p := range periods {
		if start.IsZero() || p.start.Before(start) {
			start = p.start
		}
	}
	return start, end
}

// printUnmanagedSummary prints an "Unmanaged Time" section covering
// [start, end). Time when glocker was uninstalled is a bypass of the blocking
// regime, so it is highlighted in red and labelled as a violation. Nothing is
// printed when there is no unmanaged time in range. Returns total unmanaged
// minutes.
func printUnmanagedSummary(start, end time.Time, periods []unmanagedPeriod) int {
	totalMin := calculateUnmanagedMinutesInRange(start, end, periods)
	if totalMin <= 0 {
		return 0
	}

	// Count the periods overlapping the range.
	overlapping := 0
	for _, p := range periods {
		pEnd := p.end
		if pEnd.IsZero() {
			pEnd = time.Now()
		}
		if start.Before(pEnd) && end.After(p.start) {
			overlapping++
		}
	}

	fmt.Printf("\n── %sUnmanaged Time (counts as violation)%s ──\n", colorRed, colorReset)
	fmt.Printf("  %s⚠ %s unmanaged across %d period(s)%s\n",
		colorRed, formatUnmanagedDuration(totalMin), overlapping, colorReset)
	if reasons := unmanagedReasonsForRange(start, end, periods); len(reasons) > 0 {
		fmt.Printf("  Reasons: %s\n", strings.Join(reasons, ", "))
	}
	return totalMin
}
