package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR 2026-08-13-boundary-authoring D4: the `boundaries-author` skill.
//
// Same doctrine as docdrift_test.go — every claim pinned here is one whose
// wrong version would change what an agent DOES. The authoring loop is the
// highest-risk surface in the repo to let rot, because its whole purpose is
// stopping an agent from shipping a rule that silently measures 12%.

func skillFile(t *testing.T, rel string) string {
	t.Helper()
	return repoFile(t, filepath.Join("internal", "skills", "bundled", "ctx-optimize", rel))
}

// flow collapses whitespace runs so a prose claim still matches after the
// paragraph is re-wrapped. The guard is about the CLAIM surviving, not the
// line breaks — a test that fails on reflow trains people to delete tests.
func flow(s string) string { return strings.Join(strings.Fields(s), " ") }

// The measured-or-invalid law and its two originating failures. An agent that
// learns the loop but not WHY will treat the verified block as paperwork and
// skip it under time pressure — which is exactly how the 12% rule shipped.
func TestBoundaryAuthoringLawIsOnTheAgentSurface(t *testing.T) {
	doc := flow(skillFile(t, "references/boundaries-authoring.md"))

	for _, c := range []struct{ needle, why string }{
		{"137", "the recall failure (16 of 137 through a wrapper) is the reason the loop exists; without the number it is an unmotivated rule"},
		{"vendored", "the precision failure (a vendored corpus reported as production egress) is why exclude.path is mandatory"},
		{"known_misses", "silence that looks like completeness is the one failure that makes the graph lie"},
		{"verified", "a rule without its measurement is invalid by definition"},
		{"data, never code", "the generator emits config; taking the adapter door needs written justification"},
	} {
		if !strings.Contains(doc, c.needle) {
			t.Errorf("boundaries-authoring.md lost %q — %s", c.needle, c.why)
		}
	}

	// The tier ladder must appear with all three bands. A doc that states only
	// "derive the tier" lets an agent assert EXTRACTED on a 0.7 rule.
	for _, band := range []string{"0.95", "0.70", "EXTRACTED", "INFERRED", "AMBIGUOUS"} {
		if !strings.Contains(doc, band) {
			t.Errorf("boundaries-authoring.md is missing tier band %q — the tier becomes assertable again", band)
		}
	}

	// Every step of D4's fixed loop, by name.
	for _, step := range []string{"SURVEY", "PROPOSE", "GROUND", "RUN", "MEASURE", "ITERATE", "TIER", "WRITE"} {
		if !strings.Contains(doc, step) {
			t.Errorf("boundaries-authoring.md dropped loop step %q — D4 requires all eight", step)
		}
	}
}

// Ground truth that is narrower than or equal to the rule's own regex reports
// recall 1.00 by construction. That trap is subtle enough that an agent will
// walk into it unless the doc names it.
func TestGroundTruthIndependenceExplained(t *testing.T) {
	doc := flow(skillFile(t, "references/boundaries-authoring.md"))
	if !strings.Contains(doc, "broader than your rule") && !strings.Contains(doc, "BROADER") {
		t.Error("boundaries-authoring.md never warns that ground truth must be broader than the rule — recall becomes 1.00 by construction")
	}
	if !strings.Contains(doc, "ctx-optimize search") {
		t.Error("boundaries-authoring.md does not route ground truth to `search` — an agent will reach for rg/grep, which is not cross-OS and walks a different file set")
	}
}

// Secrets: the skill may capture credential NAMES and must never touch values.
func TestSecretHandlingPinnedInAuthoringSkill(t *testing.T) {
	doc := flow(skillFile(t, "references/boundaries-authoring.md"))
	if !strings.Contains(doc, "when_identifier_matches") {
		t.Error("boundaries-authoring.md omits the flag mechanism — sensitive env names would ship unflagged")
	}
	for _, want := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD"} {
		if !strings.Contains(doc, want) {
			t.Errorf("boundaries-authoring.md lost the %q pattern from the sensitive-flag guidance", want)
		}
	}
	if !strings.Contains(doc, "NAME only") {
		t.Error("boundaries-authoring.md does not state the by-NAME-only rule for secrets")
	}
}

// The doc quotes the rule schema. If a field name drifts from the Go struct,
// every rule an agent authors from this doc fails the fail-closed loader —
// so pin the doc against defaults.json itself rather than against prose.
func TestAuthoringSchemaMatchesShippedDefaults(t *testing.T) {
	doc := flow(skillFile(t, "references/boundaries-authoring.md"))

	raw := repoFile(t, filepath.Join("internal", "boundaries", "defaults.json"))
	var f struct {
		Boundaries []map[string]json.RawMessage `json:"boundaries"`
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("defaults.json unreadable: %v", err)
	}
	if len(f.Boundaries) == 0 {
		t.Fatal("defaults.json has no rules")
	}

	// Every top-level key the shipped rules actually use must be documented.
	used := map[string]bool{}
	for _, r := range f.Boundaries {
		for k := range r {
			used[k] = true
		}
	}
	for k := range used {
		if !strings.Contains(doc, `"`+k+`"`) {
			t.Errorf("boundaries-authoring.md never shows the %q field, but shipped rules use it — an agent authoring from this doc writes an incomplete rule", k)
		}
	}

	// `exclude` is TOP-LEVEL, not nested under `when`. Getting this wrong is a
	// fail-closed loader error on the agent's first attempt.
	if !strings.Contains(doc, `"when": { "ext"`) {
		t.Error("boundaries-authoring.md does not show the `when.ext` shape")
	}
	if !strings.Contains(doc, "top-level") {
		t.Error("boundaries-authoring.md does not flag that `exclude` is top-level, not inside `when` — the most likely authoring mistake")
	}

	// The reserved-metadata contract is enforced fail-closed by the schema door.
	if !strings.Contains(doc, "namespaced") {
		t.Error("boundaries-authoring.md omits the namespaced-metadata rule — a bare unknown key is rejected fail-closed")
	}
	// scope is computed by join; an agent that sets it by hand is asserting.
	if !strings.Contains(doc, "NOT yours to set") && !strings.Contains(doc, "computed") {
		t.Error("boundaries-authoring.md does not say `scope` is computed by JOIN — an agent will hand-assert internal/external")
	}
}

// The skill is only reachable if SKILL.md and the router point at it.
func TestAuthoringSkillIsRoutable(t *testing.T) {
	skill := skillFile(t, "SKILL.md")
	routing := skillFile(t, "references/activation-routing.xml")

	if !strings.Contains(skill, "boundaries-authoring.md") {
		t.Error("SKILL.md never references boundaries-authoring.md — the deep guide is unreachable")
	}
	if !strings.Contains(routing, "boundaries-authoring.md") {
		t.Error("activation-routing.xml never routes to boundaries-authoring.md")
	}
	// Reading ports is a different intent from authoring rules; both must route.
	if !strings.Contains(routing, `id="ports"`) {
		t.Error("activation-routing.xml has no `ports` route — 'what external APIs do we call' has nowhere to land")
	}
	if !strings.Contains(routing, `id="boundaries-author"`) {
		t.Error("activation-routing.xml has no `boundaries-author` route")
	}
	// The port tier caveat: a port list is a floor, not a census.
	if !strings.Contains(routing, "FLOOR") {
		t.Error("activation-routing.xml does not warn that a port list is a FLOOR — an agent will report the egress footprint as complete")
	}
}

// The bundle must actually ship the file: an embedded skill that references a
// missing path sends the agent to a dead link.
func TestAuthoringDocIsEmbeddedInTheBundle(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "skills", skillName)
	if err := InstallDir(dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "references", "boundaries-authoring.md")); err != nil {
		t.Fatalf("boundaries-authoring.md is not in the installed bundle: %v", err)
	}
}
