package boundaries

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ruleWithGT writes a repo boundaries.json holding ONE rule whose ground truth
// is a deliberately broader sweep than the rule itself — the shape every real
// rule has, and the only shape where recall means anything.
func ruleWithGT(t *testing.T, root, id, ruleRe, gtRe, ext string, extra string) {
	t.Helper()
	v := `{"at":"2026-08-14","ground_truth":{"tool":"ctx-optimize search","cmd":"x","re":` +
		mustJSON(t, gtRe) + `,"ext":["` + ext + `"],"corpora":["fixture"]},` +
		`"expected":99,"matched":50,"recall":0.5,"sampled":1,"confirmed":1,"precision":1.0,` +
		`"known_misses":["fixture"]}`
	body := `{"version":1,"boundaries":[{"id":"` + id + `","transport":"config.env",` +
		`"direction":"consumes","when":{"ext":["` + ext + `"]},` + extra +
		`"match":[{"re":` + mustJSON(t, ruleRe) + `,"identifier":1}],` +
		`"tier":"INFERRED","verified":` + v + `}]}`
	write(t, root, ".ctxoptimize/boundaries.json", body)
}

func mustJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func result(t *testing.T, rep *Report, id string) RuleResult {
	t.Helper()
	for _, r := range rep.Rules {
		if r.Rule == id {
			return r
		}
	}
	t.Fatalf("rule %q missing from report", id)
	return RuleResult{}
}

// The core measurement: 3 ground-truth sites, the rule captures 2 (the third
// passes a variable, the classic known_miss), so recall is 2/3 and the missed
// SITE is named — a bare number would not tell an author what to fix.
func TestVerifyMeasuresLocalRecallAndNamesTheMisses(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-env", `getenv\("([A-Z_]+)"\)`, `getenv\(`, ".go", "")
	write(t, root, "a.go", "x := getenv(\"ALPHA\")\ny := getenv(\"BETA\")\nz := getenv(nameVar)\n")

	rep, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, rep, "fx-env")
	if r.Expected != 3 || r.Matched != 2 {
		t.Fatalf("want 2/3, got %d/%d", r.Matched, r.Expected)
	}
	if r.Recall == nil || *r.Recall < 0.66 || *r.Recall > 0.67 {
		t.Fatalf("recall = %v, want ~0.667", r.Recall)
	}
	if r.UnmatchedTotal != 1 || len(r.Unmatched) != 1 || r.Unmatched[0].Line != 3 {
		t.Fatalf("the variable-name site must be reported by file:line, got %+v", r.Unmatched)
	}
	// The shipped claim is carried as provenance, never as this run's result.
	if r.ClaimedRecall == nil || *r.ClaimedRecall != 0.5 {
		t.Fatalf("claimed recall not carried through: %v", r.ClaimedRecall)
	}
	if len(r.ClaimedOn) != 1 || r.ClaimedOn[0] != "fixture" {
		t.Fatalf("corpora provenance lost: %v", r.ClaimedOn)
	}
	if r.Status != StatusOK {
		t.Fatalf("status = %q", r.Status)
	}
}

// A rule that never ran has NOT passed. This is the whole reason the summary
// counts three buckets instead of pass/fail.
func TestVerifyUnexercisedIsNotAPass(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-none", `getenv\("([A-Z_]+)"\)`, `getenv\(`, ".go", "")
	write(t, root, "readme.txt", "nothing to see")

	rep, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, rep, "fx-none")
	if r.Status != StatusUnexercised {
		t.Fatalf("status = %q, want unexercised", r.Status)
	}
	if r.Recall != nil {
		t.Fatal("an unexercised rule must not report a recall")
	}
	if rep.Unexercised == 0 || rep.Exercised != 0 {
		t.Fatalf("unexercised must be counted apart from passes: %+v", rep)
	}
}

// No machine-readable ground truth = cannot verify, and says so. It must never
// be silently counted as a pass.
func TestVerifyWithoutMachineGroundTruthIsUnverifiable(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"fx-nogt","transport":"config.env","direction":"consumes",
	   "when":{"ext":[".go"]},"match":[{"re":"getenv\\(\"([A-Z_]+)\"\\)","identifier":1}],
	   "verified":{"at":"2026-08-14","ground_truth":{"tool":"eyeball","cmd":"by hand"},
	   "expected":1,"matched":1,"recall":1.0}}]}`)
	write(t, root, "a.go", `x := getenv("ALPHA")`)

	rep, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, rep, "fx-nogt")
	if r.Status != StatusUnverifiable || !strings.Contains(r.Note, "machine-readable") {
		t.Fatalf("want unverifiable with a reason, got %q / %q", r.Status, r.Note)
	}
	if rep.Unverifiable != 1 {
		t.Fatalf("unverifiable not counted: %+v", rep)
	}
}

// D3's headline case: the repo grows a site the rule cannot see, recall drops
// below the committed floor, and --strict has something to fail on.
func TestVerifyDetectsRegressionAgainstRecordedFloor(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-drift", `getenv\("([A-Z_]+)"\)`, `getenv\(`, ".go", "")
	write(t, root, "a.go", "x := getenv(\"ALPHA\")\n")

	rep, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	if r := result(t, rep, "fx-drift"); r.Recall == nil || *r.Recall != 1.0 {
		t.Fatalf("baseline run should be 1.00, got %v", r.Recall)
	}
	if _, err := RecordBaseline(root, rep); err != nil {
		t.Fatal(err)
	}

	// New code the rule cannot see — the 14-new-exec-sites scenario.
	write(t, root, "b.go", "y := getenv(varName)\n")
	rep2, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, rep2, "fx-drift")
	if r.Status != StatusRegressed {
		t.Fatalf("status = %q, want regressed (recall %s vs floor %s)", r.Status, Pct(r.Recall), Pct(r.BaselineRecall))
	}
	if r.BaselineRecall == nil || *r.BaselineRecall != 1.0 {
		t.Fatalf("floor not carried: %v", r.BaselineRecall)
	}
	if rep2.Regressed != 1 || !rep2.HasBaseline {
		t.Fatalf("report summary wrong: %+v", rep2)
	}

	// And improvement is distinguishable from a pass, so a raised floor is a
	// visible, reviewable event rather than a silent one.
	if err := os.Remove(filepath.Join(root, "b.go")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "c.go", "z := getenv(\"GAMMA\")\n")
	rep3, _ := Verify(root)
	if s := result(t, rep3, "fx-drift").Status; s != StatusOK && s != StatusImproved {
		t.Fatalf("recovered run should not be regressed, got %q", s)
	}
}

// The rule's scope exclusions apply to BOTH sides. Scoring a rule as missing
// sites it deliberately refuses to look at would punish exactly the precision
// decision ADR 2 demands (the vendored-corpus false positive).
func TestVerifyAppliesRuleExcludesToGroundTruthToo(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-ex", `getenv\("([A-Z_]+)"\)`, `getenv\(`, ".go",
		`"exclude":{"path":["vendored/"]},`)
	write(t, root, "a.go", "x := getenv(\"ALPHA\")\n")
	write(t, root, "vendored/dep.go", "y := getenv(nameVar)\n")

	rep, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, rep, "fx-ex")
	if r.Expected != 1 || r.Matched != 1 {
		t.Fatalf("excluded tree must count for NEITHER side, got %d/%d", r.Matched, r.Expected)
	}
	if r.Recall == nil || *r.Recall != 1.0 {
		t.Fatalf("recall = %v, want 1.00", r.Recall)
	}
}

// Rule fires where the ground truth does not: the sweep is narrower than the
// rule, which is a reviewer's problem, not a silent one.
func TestVerifyReportsRuleSitesOutsideTheGroundTruth(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-extra", `(?:getenv|GETENV)\("([A-Z_]+)"\)`, `getenv\(`, ".go", "")
	write(t, root, "a.go", "x := getenv(\"ALPHA\")\ny := GETENV(\"BETA\")\n")

	rep, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	if r := result(t, rep, "fx-extra"); r.Extra != 1 {
		t.Fatalf("extra = %d, want 1 (the GETENV site the sweep misses)", r.Extra)
	}
}

func TestRecordBaselineSkipsRulesThatNeverRan(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-rec", `getenv\("([A-Z_]+)"\)`, `getenv\(`, ".go", "")
	write(t, root, "a.go", "x := getenv(\"ALPHA\")\n")

	rep, _ := Verify(root)
	p, err := RecordBaseline(root, rep)
	if err != nil {
		t.Fatal(err)
	}
	var b Baseline
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Rules["fx-rec"]; !ok {
		t.Fatal("exercised rule missing from the floor")
	}
	for id := range b.Rules {
		if id != "fx-rec" {
			t.Fatalf("floor pinned for %q, which never ran — a number with nothing behind it", id)
		}
	}
}

func TestMalformedBaselineFailsLoud(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	ruleWithGT(t, root, "fx-bad", `getenv\("([A-Z_]+)"\)`, `getenv\(`, ".go", "")
	write(t, root, ".ctxoptimize/"+BaselineFile, `{"version":1,"rules":{},"typo":true}`)
	if _, err := Verify(root); err == nil {
		t.Fatal("unknown field in the floor file accepted silently")
	}
}

func TestVerifyDeterministicOrder(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, "a.go", "x := os.Getenv(\"ALPHA\")\n_ = exec.Command(\"git\")\n")
	r1, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := Verify(root)
	if len(r1.Rules) != len(r2.Rules) {
		t.Fatal("rule count varies between runs")
	}
	for i := range r1.Rules {
		if r1.Rules[i].Rule != r2.Rules[i].Rule {
			t.Fatal("rule order varies between runs")
		}
	}
}

// Guards the data added to defaults.json: every shipped rule must be
// verifiable by machine, and its machine regex must be the same one the human
// `cmd` documents — otherwise `verify` and a hand-run `search` could disagree.
func TestShippedDefaultsCarryReproducibleGroundTruth(t *testing.T) {
	hermetic(t)
	rules, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 {
		t.Fatal("no embedded defaults")
	}
	for _, r := range rules {
		var vb verifiedBlock
		if len(r.Verified) == 0 {
			t.Fatalf("%s: no verified block — no evidence, no rule (ADR 2 D1)", r.ID)
		}
		if err := json.Unmarshal(r.Verified, &vb); err != nil {
			t.Fatalf("%s: verified block does not parse: %v", r.ID, err)
		}
		if vb.GroundTruth.Re == "" {
			t.Fatalf("%s: ground_truth.re missing — `boundaries verify` could not re-run it", r.ID)
		}
		if _, err := regexp.Compile(vb.GroundTruth.Re); err != nil {
			t.Fatalf("%s: ground_truth.re does not compile: %v", r.ID, err)
		}
		if !strings.Contains(vb.GroundTruth.Cmd, vb.GroundTruth.Re) {
			t.Fatalf("%s: machine regex %q is not the one the human cmd documents (%q)",
				r.ID, vb.GroundTruth.Re, vb.GroundTruth.Cmd)
		}
		if len(vb.GroundTruth.Ext) == 0 {
			t.Fatalf("%s: ground_truth.ext missing", r.ID)
		}
		if vb.Recall != nil && *vb.Recall < 1.0 && len(vb.KnownMisses) == 0 {
			t.Fatalf("%s: recall %.2f < 1.0 with no known_misses — silence that looks like completeness (ADR 2 D1)",
				r.ID, *vb.Recall)
		}
	}
}
