package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Doc drift guard. This repo states behaviour across 13 hand-maintained surfaces
// (~2,900 lines) plus 6 site pages and 38 ADRs, and it has already been bitten
// twice in one day: references/extending.md claimed a `packConfig` field list
// that 0.10.0 changed, and the skill said nothing about redaction shipped in
// 0.9.2. Restating a rule in seven places is how it rots.
//
// So behaviour claims that MUST stay in sync get pinned here. This is cheap and
// it fails on the edit that drifts — the same idea as cljgo's
// `gen-editors --check`. It is not a style checker: every entry below is a claim
// that would MISLEAD AN AGENT if it went stale.
//
// Adding a rule: only if a wrong version of the sentence would change what an
// agent does. Otherwise leave the docs alone.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("doc surface missing: %s (%v)", rel, err)
	}
	return string(b)
}

// The AMBIGUOUS contract (ADR 2026-07-25-abstain-out-loud) is the highest-risk
// claim in the repo: get it wrong in a doc and an agent either treats a maybe as
// a caller, or reports an incomplete caller list as complete.
func TestAmbiguousContractNotDrifted(t *testing.T) {
	// The old behaviour — a bare "ambiguous dropped" — is now FALSE. Anything
	// asserting it without also saying the candidates are shortlisted/reported
	// would send an agent back to trusting a short `called by`.
	for _, rel := range []string{"CLAUDE.md", "internal/extract/code/code.go"} {
		body := repoFile(t, rel)
		for _, stale := range []string{"ambiguous dropped", "ambiguous names are dropped", "Ambiguous and unknown names are dropped"} {
			if strings.Contains(body, stale) {
				t.Errorf("%s still claims %q — ambiguous callees are now shortlisted as AMBIGUOUS and reported, not silently dropped", rel, stale)
			}
		}
	}

	// The agent-facing surfaces must carry the consequence, not just the fact.
	skill := repoFile(t, "internal/skills/bundled/ctx-optimize/SKILL.md")
	if !strings.Contains(skill, "unattributed callers") {
		t.Error("SKILL.md does not tell the agent what `unattributed callers: N` means — it will read `called by` as complete")
	}
	routing := repoFile(t, "internal/skills/bundled/ctx-optimize/references/activation-routing.xml")
	for _, want := range []string{"AMBIGUOUS", "INCOMPLETE BY DESIGN"} {
		if !strings.Contains(routing, want) {
			t.Errorf("activation-routing.xml is missing %q — the card's abstention needs the routing note", want)
		}
	}

	// VISION.md's original line green-lit graphify-parity AMBIGUOUS emission.
	// The motto reversed it; if the refinement marker disappears, the green
	// light is back and the next person ships guessed edges into blast radii.
	vision := repoFile(t, "docs/VISION.md")
	if strings.Contains(vision, "AMBIGUOUS and let the agent weigh them (graphify-parity behaviour).") {
		t.Error("docs/VISION.md still green-lights unfiltered AMBIGUOUS emission — the 2026-07-25 refinement was reverted")
	}
	if !strings.Contains(vision, "2026-07-25-abstain-out-loud") {
		t.Error("docs/VISION.md lost the pointer to the ADR that refined its own decision")
	}
}

// ADR 2026-07-25-method-call-resolution added a SECOND reason to abstain, and
// the two are settled by different greps. A surface that explains only the
// name-collision case tells an agent something false about a method whose name
// is unique — the exact failure the ADR exists to close.
func TestUnresolvedReceiverExplainedToAgents(t *testing.T) {
	skill := repoFile(t, "internal/skills/bundled/ctx-optimize/SKILL.md")
	if !strings.Contains(skill, "receiver") {
		t.Error("SKILL.md never explains the unresolved-receiver abstention — an agent will read an empty method caller list as 'nothing calls it'")
	}
	routing := repoFile(t, "internal/skills/bundled/ctx-optimize/references/activation-routing.xml")
	for _, want := range []string{"unresolved-receiver", "name-collision"} {
		if !strings.Contains(routing, want) {
			t.Errorf("activation-routing.xml is missing the %q reason — the agent cannot pick the grep that settles it", want)
		}
	}
}

// ADR 2026-07-26-include-ambiguous. An agent that knows the answer is a FLOOR
// but not that a flag exists will either under-report or fall straight back to
// grep. Both agent surfaces must name the flag AND the marking, because a
// widened row presented as a caller is the failure the flag is designed around.
func TestIncludeAmbiguousDocumentedForAgents(t *testing.T) {
	for _, rel := range []string{
		"internal/skills/bundled/ctx-optimize/SKILL.md",
		"internal/skills/bundled/ctx-optimize/references/activation-routing.xml",
	} {
		body := repoFile(t, rel)
		if !strings.Contains(body, "--include-ambiguous") {
			t.Errorf("%s never mentions --include-ambiguous — the agent cannot get past the abstention without leaving the tool", rel)
		}
		if !strings.Contains(body, "MAYBE") {
			t.Errorf("%s names the flag without the marking convention — a widened row could be reported as a caller", rel)
		}
	}
}

// Verbs and config keys shipped 2026-07-26. Each is here because an agent that
// does not know it exists behaves WORSE than one that does — it will re-derive a
// settled abstention every session, or fail to warn about an incomplete store, or
// hand the user a `rm -rf` because it thinks there is no delete verb.
//
// A doc audit that day found five things shipped with ZERO agent-surface
// coverage. This is the guard that makes that a test failure instead of a
// discovery months later.
func TestNewSurfacesReachTheAgent(t *testing.T) {
	skill := repoFile(t, "internal/skills/bundled/ctx-optimize/SKILL.md")
	routing := repoFile(t, "internal/skills/bundled/ctx-optimize/references/activation-routing.xml")
	card := repoFile(t, "internal/project/templates/instructions.md")

	for _, c := range []struct {
		what, needle, why string
		in                []string
	}{
		{"store delete", "store delete",
			"without it an agent reaches for `rm -rf ~/ctxoptimize/...`, aimed at a root holding every repo's store",
			[]string{skill, routing}},
		{"add --rebuild", "--rebuild",
			"a retired producer's nodes survive every incremental gather; an agent with no --rebuild will keep re-running `add` and keep seeing them",
			[]string{skill, routing, card}},
		{"declared resolutions", "external_methods",
			"otherwise every session re-derives the same abstention the repo already settled",
			[]string{skill, routing, card}},
		{"partial store (fresh exit 3)", "PARTIAL",
			"an agent that does not know exit 3 exists will answer from a store with a producer missing and call it fresh",
			[]string{skill, routing, card}},
	} {
		for i, body := range c.in {
			if !strings.Contains(body, c.needle) {
				t.Errorf("%s missing from agent surface #%d — %s", c.what, i, c.why)
			}
		}
	}
}

// The CORE PROMISE (owner-directed 2026-07-26): "we do not invent structure that
// isn't there — if it cannot be parsed honestly it is not indexed, and you are
// told to grep."
//
// This is the sentence every abstention in the tool is an instance of, so it is
// the highest-value thing on the agent surface and the easiest to lose in an
// edit. An agent that does not know `.txt` holds only a filename will either
// report "not found" for content that is right there in the file, or quietly
// grep without saying why the store could not answer — and the second is worse,
// because it hides a deliberate design decision behind what looks like a miss.
func TestCorePromiseIsOnTheAgentSurface(t *testing.T) {
	skill := repoFile(t, "internal/skills/bundled/ctx-optimize/SKILL.md")
	for _, want := range []string{
		"do not invent structure",
		"grep it and say so",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md lost %q — the promise is what makes every abstention legible", want)
		}
	}
	// The .txt rule specifically: an agent must know the store holds ONLY the
	// filename, or it will read a miss as absence.
	if !strings.Contains(skill, ".txt") {
		t.Error("SKILL.md does not tell the agent that a .txt yields no sections — it will report content as not-found")
	}

	// And the promise must survive in the human doc too, with its measurement:
	// an unmeasured promise is a slogan.
	cli := repoFile(t, "docs/cli.md")
	if !strings.Contains(cli, "do not invent structure") {
		t.Error("docs/cli.md lost the core promise")
	}
	if !strings.Contains(cli, "30,289") {
		t.Error("docs/cli.md states the promise without the measurement behind it — that makes it a slogan")
	}
}

// Redaction (0.9.2) is the other claim whose absence is a security problem: an
// agent that meets `[redacted]` and does not know why will open the file, which
// re-creates the leak the redaction closed.
func TestRedactionDocumentedForAgents(t *testing.T) {
	skill := repoFile(t, "internal/skills/bundled/ctx-optimize/SKILL.md")
	if !strings.Contains(skill, "redacted") {
		t.Error("SKILL.md never mentions redaction — an agent will route around it with a file read")
	}
	if !strings.Contains(skill, "Do NOT open the file") {
		t.Error("SKILL.md mentions redaction without forbidding the file-read workaround")
	}
}
