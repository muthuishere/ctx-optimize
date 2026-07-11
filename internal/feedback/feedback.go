// Package feedback is the deterministic learning loop: the host agent saves
// how a store answer worked out (Result → memory/results.ndjson), and Reflect
// aggregates those results — exponential half-life decay, no LLM — into
// Lessons plus a byte-stable reflections/LESSONS.md. The binary never judges
// an answer; it only tallies the judgments the agent recorded.
package feedback

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// Result is one recorded answer episode: what was asked, what the store said,
// which node ids the answer cited, and how it turned out.
type Result struct {
	Question   string    `json:"question"`
	Answer     string    `json:"answer,omitempty"`
	Type       string    `json:"type,omitempty"` // query|path|explain|affected
	Nodes      []string  `json:"nodes,omitempty"`
	Outcome    string    `json:"outcome,omitempty"` // useful|dead_end|corrected
	Correction string    `json:"correction,omitempty"`
	When       time.Time `json:"when"`
}

// Correction is one recorded wrong answer with its fix — the highest-value
// lesson kind, so it is carried verbatim, never decayed away.
type Correction struct {
	Question   string    `json:"question"`
	Correction string    `json:"correction"`
	When       time.Time `json:"when"`
}

// NodeScore is a node's decayed standing across all results that cited it.
type NodeScore struct {
	Node   string  `json:"node"`
	Score  float64 `json:"score"`
	Useful int     `json:"useful"` // distinct useful results citing the node
}

// Lessons is the aggregate Reflect produces (and renders to LESSONS.md).
type Lessons struct {
	PreferredNodes []NodeScore  `json:"preferred_nodes"`
	DeadEnds       []NodeScore  `json:"dead_ends"`
	Corrections    []Correction `json:"corrections"`
}

func resultsPath(s *store.Store) string {
	return filepath.Join(s.Dir, "memory", "results.ndjson")
}

func lessonsPath(s *store.Store) string {
	return filepath.Join(s.Dir, "reflections", "LESSONS.md")
}

// Save validates and appends one result line to memory/results.ndjson.
// Append-only: episodes are history, never rewritten.
func Save(s *store.Store, r Result) error {
	if strings.TrimSpace(r.Question) == "" {
		return fmt.Errorf("reject result: question is required")
	}
	switch r.Outcome {
	case "", "useful", "dead_end", "corrected":
	default:
		return fmt.Errorf("reject result: outcome %q (useful | dead_end | corrected)", r.Outcome)
	}
	if r.Outcome == "corrected" && strings.TrimSpace(r.Correction) == "" {
		return fmt.Errorf("reject result: outcome corrected requires --correction")
	}
	if err := os.MkdirAll(filepath.Dir(resultsPath(s)), 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(resultsPath(s), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Close()
}

// Results loads all saved results (absent file → empty slice, not an error).
func Results(s *store.Store) ([]Result, error) {
	f, err := os.Open(resultsPath(s))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Result
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var r Result
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", resultsPath(s), err)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// Reflect aggregates saved results into Lessons and renders them to
// reflections/LESSONS.md. Each result carries weight 0.5^(ageDays/halfLifeDays)
// at `now` (injected so tests are deterministic): useful adds it to every
// cited node, dead_end and corrected subtract it; corrected also records the
// correction text. Preferred nodes need positive score AND at least
// minCorroboration distinct useful results — one lucky hit is not a lesson.
// Output is byte-stable given the same results and now.
func Reflect(s *store.Store, now time.Time, halfLifeDays float64, minCorroboration int) (*Lessons, error) {
	if halfLifeDays <= 0 {
		return nil, fmt.Errorf("half-life must be > 0 days, got %g", halfLifeDays)
	}
	if minCorroboration < 1 {
		minCorroboration = 1
	}
	results, err := Results(s)
	if err != nil {
		return nil, err
	}

	scores := map[string]float64{}
	useful := map[string]int{}
	lessons := &Lessons{PreferredNodes: []NodeScore{}, DeadEnds: []NodeScore{}, Corrections: []Correction{}}
	for _, r := range results {
		age := now.Sub(r.When).Hours() / 24
		if age < 0 {
			age = 0 // future-dated results count as fresh, never as >1 weight
		}
		w := math.Pow(0.5, age/halfLifeDays)
		switch r.Outcome {
		case "useful":
			for _, n := range r.Nodes {
				scores[n] += w
				useful[n]++
			}
		case "dead_end":
			for _, n := range r.Nodes {
				scores[n] -= w
			}
		case "corrected":
			for _, n := range r.Nodes {
				scores[n] -= w
			}
			lessons.Corrections = append(lessons.Corrections, Correction{
				Question: r.Question, Correction: r.Correction, When: r.When,
			})
		}
	}

	for node, score := range scores {
		switch {
		case score > 0 && useful[node] >= minCorroboration:
			lessons.PreferredNodes = append(lessons.PreferredNodes, NodeScore{Node: node, Score: score, Useful: useful[node]})
		case score < 0:
			lessons.DeadEnds = append(lessons.DeadEnds, NodeScore{Node: node, Score: score, Useful: useful[node]})
		}
	}
	sort.Slice(lessons.PreferredNodes, func(i, j int) bool {
		a, b := lessons.PreferredNodes[i], lessons.PreferredNodes[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Node < b.Node
	})
	sort.Slice(lessons.DeadEnds, func(i, j int) bool {
		a, b := lessons.DeadEnds[i], lessons.DeadEnds[j]
		if a.Score != b.Score {
			return a.Score < b.Score // most negative first
		}
		return a.Node < b.Node
	})
	sort.Slice(lessons.Corrections, func(i, j int) bool {
		a, b := lessons.Corrections[i], lessons.Corrections[j]
		if !a.When.Equal(b.When) {
			return a.When.Before(b.When)
		}
		return a.Question < b.Question
	})

	doc := renderLessons(lessons, now, halfLifeDays, minCorroboration)
	if err := os.MkdirAll(filepath.Dir(lessonsPath(s)), 0o755); err != nil {
		return nil, fmt.Errorf("create reflections dir: %w", err)
	}
	if err := writeAtomic(lessonsPath(s), []byte(doc)); err != nil {
		return nil, err
	}
	return lessons, nil
}

// renderLessons is a pure string builder — sorted inputs in, stable bytes out.
func renderLessons(l *Lessons, now time.Time, halfLifeDays float64, minCorroboration int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Lessons\n\n")
	fmt.Fprintf(&sb, "_Generated %s · half-life %gd · min corroboration %d_\n",
		now.UTC().Format(time.RFC3339), halfLifeDays, minCorroboration)

	fmt.Fprintf(&sb, "\n## Preferred nodes\n\n")
	if len(l.PreferredNodes) == 0 {
		fmt.Fprintf(&sb, "(none)\n")
	}
	for _, ns := range l.PreferredNodes {
		fmt.Fprintf(&sb, "- `%s` — score %.3f (%d useful)\n", ns.Node, ns.Score, ns.Useful)
	}

	fmt.Fprintf(&sb, "\n## Dead ends\n\n")
	if len(l.DeadEnds) == 0 {
		fmt.Fprintf(&sb, "(none)\n")
	}
	for _, ns := range l.DeadEnds {
		fmt.Fprintf(&sb, "- `%s` — score %.3f\n", ns.Node, ns.Score)
	}

	fmt.Fprintf(&sb, "\n## Corrections\n\n")
	if len(l.Corrections) == 0 {
		fmt.Fprintf(&sb, "(none)\n")
	}
	for _, c := range l.Corrections {
		fmt.Fprintf(&sb, "- %s: %q → %s\n", c.When.UTC().Format("2006-01-02"), c.Question, c.Correction)
	}
	return sb.String()
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
