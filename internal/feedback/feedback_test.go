package feedback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir(), "mod")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var t0 = time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)

func TestSaveValidation(t *testing.T) {
	s := openTestStore(t)

	// Question is required.
	if err := Save(s, Result{Outcome: "useful", When: t0}); err == nil {
		t.Fatal("missing question accepted")
	}
	// Outcome must be one of the known values (empty is fine).
	if err := Save(s, Result{Question: "q", Outcome: "great", When: t0}); err == nil {
		t.Fatal("bad outcome accepted")
	}
	// corrected without a correction is rejected.
	if err := Save(s, Result{Question: "q", Outcome: "corrected", When: t0}); err == nil {
		t.Fatal("corrected without correction accepted")
	}

	// Valid results append one line each, and load back intact.
	if err := Save(s, Result{Question: "where is auth", Answer: "internal/auth", Type: "query",
		Nodes: []string{"auth.go::login"}, Outcome: "useful", When: t0}); err != nil {
		t.Fatal(err)
	}
	if err := Save(s, Result{Question: "no outcome yet", When: t0}); err != nil {
		t.Fatal(err)
	}
	results, err := Results(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0].Nodes[0] != "auth.go::login" || !results[0].When.Equal(t0) {
		t.Fatalf("round-trip mangled: %+v", results[0])
	}
	data, _ := os.ReadFile(filepath.Join(s.Dir, "memory", "results.ndjson"))
	if got := strings.Count(string(data), "\n"); got != 2 {
		t.Fatalf("want 2 ndjson lines, got %d", got)
	}
}

// Decay math: a useful hit one half-life ago (weight 0.5) is outweighed by a
// fresh dead_end (weight 1.0) — the node lands in DeadEnds, not Preferred.
func TestReflectDecay(t *testing.T) {
	s := openTestStore(t)
	old := t0.Add(-30 * 24 * time.Hour)
	must(t, Save(s, Result{Question: "q1", Nodes: []string{"n1"}, Outcome: "useful", When: old}))
	must(t, Save(s, Result{Question: "q2", Nodes: []string{"n1"}, Outcome: "dead_end", When: t0}))

	l, err := Reflect(s, t0, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.PreferredNodes) != 0 {
		t.Fatalf("stale useful survived a fresh dead_end: %+v", l.PreferredNodes)
	}
	if len(l.DeadEnds) != 1 || l.DeadEnds[0].Node != "n1" {
		t.Fatalf("dead ends: %+v", l.DeadEnds)
	}
	// weight(old useful) = 0.5, weight(fresh dead_end) = 1.0 → score -0.5.
	if got := l.DeadEnds[0].Score; got > -0.499 || got < -0.501 {
		t.Fatalf("score = %g, want ~-0.5", got)
	}
	// Flip the freshness and the node is preferred again.
	must(t, Save(s, Result{Question: "q3", Nodes: []string{"n1"}, Outcome: "useful", When: t0}))
	l, err = Reflect(s, t0, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.PreferredNodes) != 1 || l.PreferredNodes[0].Node != "n1" {
		t.Fatalf("preferred: %+v", l.PreferredNodes)
	}
}

// Corroboration: one useful result is not a lesson; two distinct ones are.
func TestCorroborationThreshold(t *testing.T) {
	s := openTestStore(t)
	must(t, Save(s, Result{Question: "q1", Nodes: []string{"once"}, Outcome: "useful", When: t0}))
	must(t, Save(s, Result{Question: "q2", Nodes: []string{"twice"}, Outcome: "useful", When: t0}))
	must(t, Save(s, Result{Question: "q3", Nodes: []string{"twice"}, Outcome: "useful", When: t0}))

	l, err := Reflect(s, t0, 30, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.PreferredNodes) != 1 || l.PreferredNodes[0].Node != "twice" || l.PreferredNodes[0].Useful != 2 {
		t.Fatalf("preferred: %+v", l.PreferredNodes)
	}
	// "once" is positive but uncorroborated — neither preferred nor a dead end.
	if len(l.DeadEnds) != 0 {
		t.Fatalf("dead ends: %+v", l.DeadEnds)
	}
}

// LESSONS.md is byte-stable: same results + same now → identical bytes.
func TestLessonsByteStable(t *testing.T) {
	s := openTestStore(t)
	must(t, Save(s, Result{Question: "qa", Nodes: []string{"a", "b"}, Outcome: "useful", When: t0.Add(-24 * time.Hour)}))
	must(t, Save(s, Result{Question: "qb", Nodes: []string{"a"}, Outcome: "useful", When: t0}))
	must(t, Save(s, Result{Question: "qc", Nodes: []string{"c"}, Outcome: "dead_end", When: t0}))
	must(t, Save(s, Result{Question: "qd", Nodes: []string{"c"}, Outcome: "corrected",
		Correction: "it moved to internal/pay", When: t0}))

	path := filepath.Join(s.Dir, "reflections", "LESSONS.md")
	if _, err := Reflect(s, t0, 14, 2); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reflect(s, t0, 14, 2); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("LESSONS.md not byte-stable:\n--- first\n%s\n--- second\n%s", first, second)
	}
	for _, want := range []string{"# Lessons", "## Preferred nodes", "## Dead ends", "## Corrections", "it moved to internal/pay"} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("LESSONS.md missing %q:\n%s", want, first)
		}
	}
}

// Corrected flow: the correction text surfaces in Lessons (sorted, verbatim)
// and the cited node is penalized.
func TestCorrectedFlow(t *testing.T) {
	s := openTestStore(t)
	must(t, Save(s, Result{Question: "later", Nodes: []string{"n"}, Outcome: "corrected",
		Correction: "fix B", When: t0}))
	must(t, Save(s, Result{Question: "earlier", Outcome: "corrected",
		Correction: "fix A", When: t0.Add(-48 * time.Hour)}))

	l, err := Reflect(s, t0, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Corrections) != 2 || l.Corrections[0].Correction != "fix A" || l.Corrections[1].Correction != "fix B" {
		t.Fatalf("corrections: %+v", l.Corrections)
	}
	if len(l.DeadEnds) != 1 || l.DeadEnds[0].Node != "n" || l.DeadEnds[0].Score >= 0 {
		t.Fatalf("corrected did not penalize the node: %+v", l.DeadEnds)
	}

	// Bad half-life fails loudly.
	if _, err := Reflect(s, t0, 0, 1); err == nil {
		t.Fatal("half-life 0 accepted")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
