package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end through the real dispatch: the verb must report local recall,
// name the unmatched site, refuse to call an unexercised rule a pass, and give
// CI something to fail on once a floor exists.
func TestBoundariesVerifyCLI(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_BOUNDARIES", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_SERVICES", t.TempDir())
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := t.TempDir()

	writeRepo(t, repo, map[string]string{
		".ctxoptimize/boundaries.json": `{"version":1,"boundaries":[
		  {"id":"fx-cli","transport":"config.env","direction":"consumes",
		   "when":{"ext":[".go"]},
		   "scan":"raw","match":[{"re":"getenv\\(\"([A-Z_]+)\"\\)","identifier":1}],
		   "tier":"INFERRED",
		   "verified":{"at":"2026-08-14",
		     "ground_truth":{"tool":"ctx-optimize search","cmd":"search 'getenv\\(' --ext .go --count",
		                     "re":"getenv\\(","ext":[".go"],"corpora":["fixture-corpus"]},
		     "expected":9,"matched":5,"recall":0.55,"sampled":1,"confirmed":1,"precision":1.0,
		     "known_misses":["variable name"]}}]}`,
		"a.go": "x := getenv(\"ALPHA\")\ny := getenv(varName)\n",
	})

	out, _ := runCLI(t, 0, "boundaries", "verify", "--path", repo)
	if !strings.Contains(out, "recall 0.50") {
		t.Fatalf("local recall (1 of 2) not reported:\n%s", out)
	}
	if !strings.Contains(out, "a.go:2") {
		t.Fatalf("the unmatched site must be cited by file:line:\n%s", out)
	}
	// The shipped number is provenance, explicitly not this run's result.
	if !strings.Contains(out, "fixture-corpus") || !strings.Contains(out, "not reproduced here") {
		t.Fatalf("shipped claim must be labelled with its corpora and disclaimed:\n%s", out)
	}
	// Rules with no local sites are counted apart from passes.
	if !strings.Contains(out, "no sites here") {
		t.Fatalf("unexercised rules must be visible:\n%s", out)
	}

	// --json carries the same facts in machine form.
	jout, _ := runCLI(t, 0, "boundaries", "verify", "--path", repo, "--json")
	var rep struct {
		Rules []struct {
			Rule           string   `json:"rule"`
			Status         string   `json:"status"`
			Matched        int      `json:"matched"`
			Expected       int      `json:"expected"`
			Recall         *float64 `json:"recall"`
			ClaimedRecall  *float64 `json:"claimed_recall"`
			UnmatchedTotal int      `json:"unmatched_total"`
		} `json:"rules"`
		Exercised   int  `json:"exercised"`
		Unexercised int  `json:"unexercised"`
		Regressed   int  `json:"regressed"`
		HasBaseline bool `json:"has_baseline"`
	}
	if err := json.Unmarshal([]byte(jout), &rep); err != nil {
		t.Fatalf("--json is not valid JSON: %v", err)
	}
	var found bool
	for _, r := range rep.Rules {
		if r.Rule != "fx-cli" {
			continue
		}
		found = true
		if r.Matched != 1 || r.Expected != 2 || r.Recall == nil || *r.Recall != 0.5 {
			t.Fatalf("json rule wrong: %+v", r)
		}
		if r.ClaimedRecall == nil || *r.ClaimedRecall != 0.55 {
			t.Fatal("claimed recall must stay distinct from the measured one")
		}
	}
	if !found {
		t.Fatal("fx-cli missing from --json")
	}
	if rep.HasBaseline || rep.Unexercised == 0 {
		t.Fatalf("summary wrong before any floor exists: %+v", rep)
	}

	// --strict is quiet until a floor exists to fall below (exit 0).
	runCLI(t, 0, "boundaries", "verify", "--path", repo, "--strict")
	rout, _ := runCLI(t, 0, "boundaries", "verify", "--path", repo, "--record")
	if !strings.Contains(rout, "recorded local recall floor") {
		t.Fatalf("--record said nothing useful:\n%s", rout)
	}
	if _, err := os.Stat(filepath.Join(repo, ".ctxoptimize", "boundaries-baseline.json")); err != nil {
		t.Fatalf("floor file not written: %v", err)
	}

	// Now regress it: new code the rule cannot see. --strict must exit nonzero,
	// AND the drop must be visible in the report — an exit code alone tells a
	// reader nothing about what to fix.
	writeRepo(t, repo, map[string]string{"b.go": "z := getenv(other)\n"})
	sout, _ := runCLI(t, 1, "boundaries", "verify", "--path", repo, "--strict")
	if !strings.Contains(sout, "REGRESSED") {
		t.Fatalf("the drop must be visible in the report, not only the exit code:\n%s", sout)
	}
	// D3's shape verbatim: the recorded floor, an arrow, the measured number.
	// The floor is 0.50 because this fixture always had one variable-name miss;
	// the drop to 0.33 is the NEW site the rule cannot see.
	if !strings.Contains(sout, "recall 0.50 → 0.33") {
		t.Fatalf("D3's floor→measured shape missing:\n%s", sout)
	}
	if !strings.Contains(sout, "b.go:1") {
		t.Fatalf("the newly-unmatched site must be named:\n%s", sout)
	}
}
