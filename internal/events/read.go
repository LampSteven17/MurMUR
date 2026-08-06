package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Query filters for Read. Zero values mean "no filter".
type Query struct {
	MinSeverity string // debug|info|warn|escalate — includes higher severities
	Kind        string
	Agent       string
	Since       time.Time
	Limit       int // max events returned (newest kept); 0 = 500
}

var sevRank = map[string]int{SevDebug: 0, SevInfo: 1, SevWarn: 2, SevEscalate: 3}

// Read loads events from the yearly JSONL files in dir, oldest→newest,
// applying the query. It reads at most the current and previous year files —
// callers wanting deep history query Loki or read archives directly.
func Read(dir string, q Query) ([]Event, error) {
	if q.Limit <= 0 {
		q.Limit = 500
	}
	minRank := 0
	if q.MinSeverity != "" {
		r, ok := sevRank[q.MinSeverity]
		if !ok {
			return nil, fmt.Errorf("invalid severity %q", q.MinSeverity)
		}
		minRank = r
	}

	year := time.Now().UTC().Year()
	var out []Event
	for _, y := range []int{year - 1, year} {
		path := filepath.Join(dir, fmt.Sprintf("events-%d.jsonl", y))
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			var e Event
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				continue // one corrupt line must not kill the stream view
			}
			if sevRank[e.Severity] < minRank {
				continue
			}
			if q.Kind != "" && e.Kind != q.Kind {
				continue
			}
			if q.Agent != "" && e.Agent != q.Agent {
				continue
			}
			if !q.Since.IsZero() {
				if ts, err := time.Parse(time.RFC3339Nano, e.TS); err == nil && ts.Before(q.Since) {
					continue
				}
			}
			out = append(out, e)
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	if len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out, nil
}
