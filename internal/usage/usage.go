// Package usage is the served-counter: every read verb appends one ndjson
// line (what was asked, how many hits, bytes out, duration) to the store's
// metrics file. This is the observability the proof matrix ran blind
// without — "how much has the store served, and what did it save" becomes
// a number instead of a feeling. Recording is fail-silent: analytics must
// never break an answer. No network, no telemetry — the file stays in the
// local store like everything else.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const fileName = "metrics/usage.ndjson"

type Event struct {
	TS    time.Time `json:"ts"`
	Verb  string    `json:"verb"`
	Arg   string    `json:"arg,omitempty"`
	Hits  int       `json:"hits"`
	Bytes int       `json:"bytes"`
	MS    int64     `json:"ms"`
}

// Path returns the metrics file location for a store.
func Path(storeDir string) string {
	return filepath.Join(storeDir, filepath.FromSlash(fileName))
}

// Record appends one event. Errors are deliberately dropped.
func Record(storeDir string, e Event) {
	if storeDir == "" {
		return
	}
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	p := filepath.Join(storeDir, filepath.FromSlash(fileName))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	f.Write(append(b, '\n'))
}

type VerbStat struct {
	Count int   `json:"count"`
	Bytes int   `json:"bytes"`
	AvgMS int64 `json:"avg_ms"`
}

// Savings model, stated so the number is checkable: every answered read
// verb replaces the search-and-read chain the A/B baselines actually made —
// one grep (~600 tok of matches) plus two file reads (~3,500 tok each;
// S1e measured full-file pointer-chase reads at 9–14k, so 2×3.5k is
// conservative). saved = replaced − served, floored at zero per event.
// Cost uses a blended $3/M-token input rate (typical frontier input price).
const (
	replacedPerAnswer = 600 + 2*3500
	usdPerMTok        = 3.0
)

type Summary struct {
	Total      int                 `json:"total_served"`
	Bytes      int                 `json:"bytes_served"`
	EstTokens  int                 `json:"est_tokens_served"`
	EstSaved   int                 `json:"est_tokens_saved"`
	EstUSD     float64             `json:"est_cost_saved_usd"`
	SavedModel string              `json:"saved_model"`
	Today      int                 `json:"served_today"`
	Last7Days  int                 `json:"served_last_7_days"`
	ByVerb     map[string]VerbStat `json:"by_verb"`
	ByDay      map[string]int      `json:"by_day"` // YYYY-MM-DD → count
	FirstEvent string              `json:"first_event,omitempty"`
	File       string              `json:"file"` // where the raw events live
}

// Summarize folds the metrics file into totals. Missing file = zero summary.
func Summarize(storeDir string) (*Summary, error) {
	sum := &Summary{ByVerb: map[string]VerbStat{}, ByDay: map[string]int{},
		File: filepath.Join(storeDir, filepath.FromSlash(fileName))}
	f, err := os.Open(filepath.Join(storeDir, filepath.FromSlash(fileName)))
	if err != nil {
		if os.IsNotExist(err) {
			return sum, nil
		}
		return nil, err
	}
	defer f.Close()
	today := time.Now().Format("2006-01-02")
	weekAgo := time.Now().AddDate(0, 0, -7)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	totalMS := map[string]int64{}
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue // never fail a summary over one bad line
		}
		sum.Total++
		sum.Bytes += e.Bytes
		day := e.TS.Format("2006-01-02")
		sum.ByDay[day]++
		if day == today {
			sum.Today++
		}
		if e.TS.After(weekAgo) {
			sum.Last7Days++
		}
		vs := sum.ByVerb[e.Verb]
		vs.Count++
		vs.Bytes += e.Bytes
		totalMS[e.Verb] += e.MS
		sum.ByVerb[e.Verb] = vs
		if e.Verb != "hook-context" { // the nudge itself replaces nothing
			if saved := replacedPerAnswer - e.Bytes/4; saved > 0 {
				sum.EstSaved += saved
			}
		}
		if sum.FirstEvent == "" || day < sum.FirstEvent {
			sum.FirstEvent = day
		}
	}
	for v, vs := range sum.ByVerb {
		if vs.Count > 0 {
			vs.AvgMS = totalMS[v] / int64(vs.Count)
		}
		sum.ByVerb[v] = vs
	}
	sum.EstTokens = sum.Bytes / 4
	sum.EstUSD = float64(sum.EstSaved) / 1e6 * usdPerMTok
	sum.SavedModel = "each answer replaces ~1 grep (600 tok) + 2 file reads (2×3,500 tok, S1e-measured conservative); saved = replaced − served; cost at $3/M input tokens"
	return sum, sc.Err()
}
