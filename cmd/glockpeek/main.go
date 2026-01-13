// glockpeek - peek at your glocker logs
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"glocker/internal/reports"
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
	summaryFlag := flag.Bool("summary", false, "Print summary statistics")
	unblocksFlag := flag.Bool("unblocks", false, "Show unblocks summary")
	violationsFlag := flag.Bool("violations", false, "Show violations summary")
	topN := flag.Int("top", 5, "Number of top items to show")
	fromDate := flag.String("from", "", "Start date (YYYY, YYYY-MM, or YYYY-MM-DD)")
	toDate := flag.String("to", "", "End date (YYYY, YYYY-MM, or YYYY-MM-DD)")
	dayDate := flag.String("day", "", "Show detailed logs for a specific day (YYYY-MM-DD)")
	monthDate := flag.String("month", "", "Show detailed logs for a specific month (YYYY-MM)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "glockpeek - peek at your glocker logs\n\n")
		fmt.Fprintf(os.Stderr, "Usage: glockpeek [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -summary                 Show all summaries\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -unblocks                Show unblocks summary only\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -violations              Show violations summary only\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -summary -top 10         Show top 10 items\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024               Show all of 2024 onwards\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024-06            Show from June 2024 onwards\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024-06-15         Show from specific date\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024 -to 2024      Show only 2024\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -from 2024-01 -to 2024-06 Show Jan-Jun 2024\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -day 2024-06-15          Show detailed logs for a day\n")
		fmt.Fprintf(os.Stderr, "  glockpeek -month 2024-06           Show detailed logs for a month\n")
	}

	flag.Parse()

	// Handle -day flag separately (detailed view)
	if *dayDate != "" {
		day, err := time.ParseInLocation("2006-01-02", *dayDate, time.Local)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid -day date %q\n", *dayDate)
			fmt.Fprintf(os.Stderr, "Format must be YYYY-MM-DD (e.g., 2024-06-15)\n")
			os.Exit(1)
		}
		printDayDetails(day)
		return
	}

	// Handle -month flag separately (detailed view)
	if *monthDate != "" {
		month, err := time.ParseInLocation("2006-01", *monthDate, time.Local)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid -month date %q\n", *monthDate)
			fmt.Fprintf(os.Stderr, "Format must be YYYY-MM (e.g., 2024-06)\n")
			os.Exit(1)
		}
		printMonthDetails(month)
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

	// Default to summary if no specific flag
	if !*summaryFlag && !*unblocksFlag && !*violationsFlag {
		*summaryFlag = true
	}

	showUnblocks := *summaryFlag || *unblocksFlag
	showViolations := *summaryFlag || *violationsFlag

	if showUnblocks {
		printUnblocksSummary(*topN, from, to)
	}

	if showViolations {
		if showUnblocks {
			fmt.Println()
		}
		printViolationsSummary(*topN, from, to)
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

	if len(entries) == 0 {
		fmt.Println("\nNo violation entries found.")
		return
	}

	summary := reports.SummarizeReports(entries)

	fmt.Printf("\nTotal violations: %d\n", summary.TotalCount)
	if summary.FirstEntry != nil && summary.LastEntry != nil {
		fmt.Printf("Date range: %s to %s\n",
			summary.FirstEntry.Format("2006-01-02"),
			summary.LastEntry.Format("2006-01-02"))
	}

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

// coloredBar returns a colored bar string based on whether count is above average
func coloredBar(count, maxCount, avg, maxLen int) string {
	length := barLen(count, maxCount, maxLen)
	bar := strings.Repeat(barChar, length)

	if count > avg {
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

// timelineEntry represents a single event for the detailed timeline view
type timelineEntry struct {
	Time    time.Time
	Type    string // "unblock" or "violation"
	Details string
}

// printDayDetails prints detailed logs for a specific day
func printDayDetails(day time.Time) {
	dayStart := day
	dayEnd := day.Add(23*time.Hour + 59*time.Minute + 59*time.Second)

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  DETAILED LOG: %-31s ║\n", day.Format("Monday, January 2, 2006"))
	fmt.Printf("╚════════════════════════════════════════════════╝\n")

	// Collect all events into a timeline
	var timeline []timelineEntry

	// Get unblocks for this day
	unblocks, err := reports.ParseUnblocksLog("")
	if err == nil {
		unblocks = reports.FilterUnblocks(unblocks, reports.UnblockFilter{
			StartTime: &dayStart,
			EndTime:   &dayEnd,
		})
		for _, u := range unblocks {
			duration := int(u.RestoreTime.Sub(u.UnblockTime).Minutes())
			details := fmt.Sprintf("%s%s%s unblocked %s%s%s for %s%d min%s — %s\"%s\"%s",
				colorYellow, "UNBLOCK", colorReset,
				colorGreen, u.Domain, colorReset,
				colorDim, duration, colorReset,
				colorDim, u.Reason, colorReset)
			timeline = append(timeline, timelineEntry{
				Time:    u.UnblockTime,
				Type:    "unblock",
				Details: details,
			})
		}
	}

	// Get violations for this day
	violations, err := reports.ParseReportsLog("")
	if err == nil {
		violations = reports.FilterReports(violations, reports.ReportFilter{
			StartTime: &dayStart,
			EndTime:   &dayEnd,
		})
		for _, v := range violations {
			typeStr := "URL"
			if v.Type == reports.ReportTypeContent {
				typeStr = "CONTENT"
			}
			details := fmt.Sprintf("%s%s%s keyword %s%s%s on %s%s%s",
				colorRed, typeStr, colorReset,
				colorRed, v.Keyword, colorReset,
				colorDim, truncateString(v.Domain, 40), colorReset)
			timeline = append(timeline, timelineEntry{
				Time:    v.Timestamp,
				Type:    "violation",
				Details: details,
			})
		}
	}

	if len(timeline) == 0 {
		fmt.Println("\nNo events found for this day.")
		return
	}

	// Sort by time
	sortTimeline(timeline)

	// Count violations per hour to find egregious hours
	hourCounts := make(map[int]int)
	for _, e := range timeline {
		if e.Type == "violation" {
			hourCounts[e.Time.Hour()]++
		}
	}
	avgHourViolations := 0
	if len(hourCounts) > 0 {
		total := 0
		for _, c := range hourCounts {
			total += c
		}
		avgHourViolations = total / len(hourCounts)
	}

	// Print timeline
	fmt.Printf("\n%d events:\n\n", len(timeline))
	currentHour := -1
	for _, e := range timeline {
		hour := e.Time.Hour()
		if hour != currentHour {
			// Check if this hour is egregious
			hourLabel := e.Time.Format("15:00")
			if hourCounts[hour] > avgHourViolations && avgHourViolations > 0 {
				fmt.Printf("── %s%s%s %s(%d violations)%s ──\n",
					colorInverse, hourLabel, colorReset, colorRed, hourCounts[hour], colorReset)
			} else {
				fmt.Printf("── %s ──\n", hourLabel)
			}
			currentHour = hour
		}
		fmt.Printf("  %s  %s\n", e.Time.Format("15:04:05"), e.Details)
	}

	// Print summary for the day
	printDaySummary(timeline)
}

// printMonthDetails prints aggregated daily logs for a specific month
func printMonthDetails(month time.Time) {
	monthStart := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.Local)
	monthEnd := monthStart.AddDate(0, 1, 0).Add(-time.Second)

	fmt.Printf("╔════════════════════════════════════════════════╗\n")
	fmt.Printf("║  MONTHLY LOG: %-32s ║\n", month.Format("January 2006"))
	fmt.Printf("╚════════════════════════════════════════════════╝\n")

	// Get unblocks for this month
	unblocks, _ := reports.ParseUnblocksLog("")
	unblocks = reports.FilterUnblocks(unblocks, reports.UnblockFilter{
		StartTime: &monthStart,
		EndTime:   &monthEnd,
	})

	// Get violations for this month
	violations, _ := reports.ParseReportsLog("")
	violations = reports.FilterReports(violations, reports.ReportFilter{
		StartTime: &monthStart,
		EndTime:   &monthEnd,
	})

	if len(unblocks) == 0 && len(violations) == 0 {
		fmt.Println("\nNo events found for this month.")
		return
	}

	// Aggregate by day
	type dayStats struct {
		date            time.Time
		violations      int
		unblocks        int
		topKeyword      string
		topKeywordCount int
		topPeriod       string
		topPeriodCount  int
		topReason       string
		topReasonCount  int
		keywords        map[string]int
		periods         map[string]int
		reasons         map[string]int
	}

	days := make(map[string]*dayStats)

	// Process violations
	for _, v := range violations {
		dayStr := v.Timestamp.Format("2006-01-02")
		if days[dayStr] == nil {
			days[dayStr] = &dayStats{
				date:     v.Timestamp,
				keywords: make(map[string]int),
				periods:  make(map[string]int),
				reasons:  make(map[string]int),
			}
		}
		d := days[dayStr]
		d.violations++
		d.keywords[v.Keyword]++
		d.periods[getTimePeriod(v.Timestamp.Hour())]++
	}

	// Process unblocks
	for _, u := range unblocks {
		dayStr := u.UnblockTime.Format("2006-01-02")
		if days[dayStr] == nil {
			days[dayStr] = &dayStats{
				date:     u.UnblockTime,
				keywords: make(map[string]int),
				periods:  make(map[string]int),
				reasons:  make(map[string]int),
			}
		}
		d := days[dayStr]
		d.unblocks++
		d.reasons[u.Reason]++
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
		for r, c := range d.reasons {
			if c > d.topReasonCount {
				d.topReason = r
				d.topReasonCount = c
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

	// Calculate average violations per day (for days with violations)
	totalV, totalU := 0, 0
	daysWithViolations := 0
	for _, dayStr := range sortedDays {
		d := days[dayStr]
		totalV += d.violations
		totalU += d.unblocks
		if d.violations > 0 {
			daysWithViolations++
		}
	}
	avgViolations := 0
	if daysWithViolations > 0 {
		avgViolations = totalV / daysWithViolations
	}

	// Print compact daily summary
	fmt.Println()
	for _, dayStr := range sortedDays {
		d := days[dayStr]

		// Highlight egregious days (above average)
		isEgregious := d.violations > avgViolations && avgViolations > 0

		// Format: "Jan 02 Mon | V:12 (afternoon, porn) | U:2 (work)"
		datePart := fmt.Sprintf("%s %s", d.date.Format("Jan 02"), d.date.Format("Mon")[:3])
		if isEgregious {
			datePart = fmt.Sprintf("%s%s%s", colorInverse, datePart, colorReset)
		}
		line := datePart + " │"

		if d.violations > 0 {
			vColor := colorRed
			if isEgregious {
				// Inverse + red for egregious
				vColor = colorInverse + colorRed
			}
			line += fmt.Sprintf(" %sV:%d%s", vColor, d.violations, colorReset)
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
		}

		if d.unblocks > 0 {
			if d.violations > 0 {
				line += " │"
			}
			line += fmt.Sprintf(" %sU:%d%s", colorYellow, d.unblocks, colorReset)
			if d.topReason != "" {
				line += fmt.Sprintf(" %s(%s)%s", colorDim, truncateString(d.topReason, 20), colorReset)
			}
		}

		fmt.Println(line)
	}

	// Month totals
	fmt.Printf("\n── Totals: %sV:%d%s %sU:%d%s over %d days ──\n",
		colorRed, totalV, colorReset,
		colorYellow, totalU, colorReset,
		len(days))
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

// sortTimeline sorts timeline entries by time
func sortTimeline(timeline []timelineEntry) {
	for i := 0; i < len(timeline)-1; i++ {
		for j := i + 1; j < len(timeline); j++ {
			if timeline[j].Time.Before(timeline[i].Time) {
				timeline[i], timeline[j] = timeline[j], timeline[i]
			}
		}
	}
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// printDaySummary prints a summary at the end of day details
func printDaySummary(timeline []timelineEntry) {
	unblockCount := 0
	violationCount := 0
	keywords := make(map[string]int)
	domains := make(map[string]int)

	for _, e := range timeline {
		if e.Type == "unblock" {
			unblockCount++
		} else {
			violationCount++
		}
	}

	// Get keyword counts from violations
	violations, _ := reports.ParseReportsLog("")
	for _, v := range violations {
		for _, e := range timeline {
			if e.Type == "violation" && e.Time.Equal(v.Timestamp) {
				keywords[v.Keyword]++
				domains[v.Domain]++
				break
			}
		}
	}

	fmt.Println("\n── Day Summary ──")
	fmt.Printf("  Unblocks:   %d\n", unblockCount)
	fmt.Printf("  Violations: %d\n", violationCount)

	if len(keywords) > 0 {
		fmt.Print("  Top keywords: ")
		topKw := getTopFromMap(keywords, 3)
		fmt.Println(strings.Join(topKw, ", "))
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
