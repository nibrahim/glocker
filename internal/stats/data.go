package stats

import (
	"net/url"
	"sort"
	"time"

	"glocker/internal/reports"
)

// minUnmanagedDuration matches cmd/glockpeek and lib/parse.js: shorter
// uninstall→install gaps are upgrades, not real exposure.
const minUnmanagedDuration = 2 * time.Minute

type violationJSON struct {
	TS      int64  `json:"ts"`
	Type    string `json:"type"`
	Keyword string `json:"keyword"`
	URL     string `json:"url"`
	Domain  string `json:"domain"`
}

type unblockJSON struct {
	TS        int64  `json:"ts"`
	RestoreTS *int64 `json:"restoreTs"`
	Reason    string `json:"reason"`
	Domain    string `json:"domain"`
}

type lifecycleJSON struct {
	TS     int64  `json:"ts"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

type unmanagedJSON struct {
	Start  int64  `json:"start"`
	End    int64  `json:"end"`
	Open   bool   `json:"open"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

type sourcesJSON struct {
	Reports   bool `json:"reports"`
	Unblocks  bool `json:"unblocks"`
	Lifecycle bool `json:"lifecycle"`
	Usage     bool `json:"usage"`
}

type dataResponse struct {
	Now        int64           `json:"now"`
	Sources    sourcesJSON     `json:"sources"`
	Violations []violationJSON `json:"violations"`
	Unblocks   []unblockJSON   `json:"unblocks"`
	Lifecycle  []lifecycleJSON `json:"lifecycle"`
	Unmanaged  []unmanagedJSON `json:"unmanaged"`
	Usage      []usageSample   `json:"usage"`
}

// buildData assembles the full parsed history the dashboard renders. Times are
// emitted as epoch milliseconds and every slice is sorted ascending, matching
// glockpeek-web/lib/parse.js exactly.
func buildData(p logPaths, now time.Time) dataResponse {
	resp := dataResponse{
		Now:        now.UnixMilli(),
		Violations: []violationJSON{},
		Unblocks:   []unblockJSON{},
		Lifecycle:  []lifecycleJSON{},
		Unmanaged:  []unmanagedJSON{},
		Usage:      []usageSample{},
	}

	if entries, err := reports.ParseReportsLog(p.reports); err == nil {
		resp.Sources.Reports = true
		for _, e := range entries {
			domain := e.Domain
			if domain == "" {
				domain = hostFromURL(e.URL)
			}
			resp.Violations = append(resp.Violations, violationJSON{
				TS: e.Timestamp.UnixMilli(), Type: string(e.Type),
				Keyword: e.Keyword, URL: e.URL, Domain: domain,
			})
		}
		sort.Slice(resp.Violations, func(i, j int) bool { return resp.Violations[i].TS < resp.Violations[j].TS })
	}

	if entries, err := reports.ParseUnblocksLog(p.unblocks); err == nil {
		resp.Sources.Unblocks = true
		for _, e := range entries {
			if e.UnblockTime.IsZero() {
				continue
			}
			u := unblockJSON{TS: e.UnblockTime.UnixMilli(), Reason: e.Reason, Domain: e.Domain}
			if !e.RestoreTime.IsZero() {
				ms := e.RestoreTime.UnixMilli()
				u.RestoreTS = &ms
			}
			resp.Unblocks = append(resp.Unblocks, u)
		}
		sort.Slice(resp.Unblocks, func(i, j int) bool { return resp.Unblocks[i].TS < resp.Unblocks[j].TS })
	}

	var lifecycle []reports.LifecycleEntry
	if entries, err := reports.ParseLifecycleLog(p.lifecycle); err == nil {
		resp.Sources.Lifecycle = true
		lifecycle = entries
		for _, e := range entries {
			resp.Lifecycle = append(resp.Lifecycle, lifecycleJSON{
				TS: e.Timestamp.UnixMilli(), Type: e.Type, Reason: e.Reason, Note: e.Note,
			})
		}
		sort.Slice(resp.Lifecycle, func(i, j int) bool { return resp.Lifecycle[i].TS < resp.Lifecycle[j].TS })
	}
	resp.Unmanaged = unmanagedPeriods(lifecycle, now)

	if samples, ok, _ := readUsageLog(p.usage); ok {
		resp.Sources.Usage = true
		if samples != nil {
			resp.Usage = samples
		}
	}

	return resp
}

// unmanagedPeriods pairs each uninstall with the next install; an open span
// (uninstall with no following install) ends at now. Mirrors getUnmanagedPeriods
// in cmd/glockpeek/main.go and unmanagedPeriods in lib/parse.js.
func unmanagedPeriods(entries []reports.LifecycleEntry, now time.Time) []unmanagedJSON {
	out := []unmanagedJSON{}
	var open *reports.LifecycleEntry
	for i := range entries {
		e := entries[i]
		switch {
		case e.Type == "uninstall":
			open = &entries[i]
		case e.Type == "install" && open != nil:
			if e.Timestamp.Sub(open.Timestamp) >= minUnmanagedDuration {
				out = append(out, unmanagedJSON{
					Start: open.Timestamp.UnixMilli(), End: e.Timestamp.UnixMilli(),
					Open: false, Reason: open.Reason, Note: open.Note,
				})
			}
			open = nil
		}
	}
	if open != nil {
		out = append(out, unmanagedJSON{
			Start: open.Timestamp.UnixMilli(), End: now.UnixMilli(),
			Open: true, Reason: open.Reason, Note: open.Note,
		})
	}
	return out
}

func hostFromURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Hostname()
	}
	return ""
}
