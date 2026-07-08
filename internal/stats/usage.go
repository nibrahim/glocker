package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// usageActive is the focused window of a sample, as sent to the dashboard.
type usageActive struct {
	Class    string `json:"class"`
	Instance string `json:"instance,omitempty"`
	Title    string `json:"title"`
}

// usageSample mirrors the shape the frontend expects from /api/data (see
// lib/parse.js parseUsage): the active window + idle time, with ts in epoch ms.
// The full window list from the raw log is dropped here.
type usageSample struct {
	TS          int64        `json:"ts"`
	IdleMS      int64        `json:"idleMs"`
	Active      *usageActive `json:"active"`
	WindowCount int          `json:"windowCount"`
}

// rawUsageLine is one line of the usage-tracker JSONL log (internal/usage).
type rawUsageLine struct {
	TS      string `json:"ts"`
	IdleMS  *int64 `json:"idle_ms"`
	Windows []struct {
		Active   bool   `json:"active"`
		Class    string `json:"class"`
		Instance string `json:"instance"`
		Title    string `json:"title"`
	} `json:"windows"`
}

// readUsageLog parses the usage JSONL log. The bool reports whether the file
// exists (a missing file is not an error — usage tracking may simply be off).
func readUsageLog(path string) ([]usageSample, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	var out []usageSample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // samples can list many windows

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var raw rawUsageLine
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue // skip malformed lines
		}
		t, err := time.Parse(time.RFC3339Nano, raw.TS)
		if err != nil {
			continue
		}
		s := usageSample{TS: t.UnixMilli(), IdleMS: -1, WindowCount: len(raw.Windows)}
		if raw.IdleMS != nil {
			s.IdleMS = *raw.IdleMS
		}
		for i := range raw.Windows {
			if raw.Windows[i].Active {
				w := raw.Windows[i]
				s.Active = &usageActive{Class: w.Class, Instance: w.Instance, Title: w.Title}
				break
			}
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return out, true, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out, true, nil
}
