package golden

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/app"
)

// TestGoldenGroundingProbes is the anti-hallucination tier (ADR
// 2026-07-16-verify-verb): adversarial asks where the RIGHT answer is a
// refusal, a labeled fuzzy match, or a failed verification — never a
// confident answer about the wrong thing. Each probe pins the defensive
// behavior the skill teaches; a regression here means agents silently get
// wrong-symbol or unverifiable answers again.
func TestGoldenGroundingProbes(t *testing.T) {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "repos", "multimod"), repo)
	storeRoot := t.TempDir()
	gatherWithin(t, 10*time.Second, repo, storeRoot)

	run := func(wantCode int, args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		code := app.Run(append(args, "--store", storeRoot, "--path", repo), &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d)\n%s%s", args, code, wantCode, out.String(), errb.String())
		}
		return out.String() + errb.String()
	}

	// P1 — a name that exists nowhere must ABSTAIN, never answer: either the
	// total-miss suggestions or the ambiguity refusal, depending on how the
	// tokens land — both are refusals, an answer is the failure.
	out := run(1, "card", "ChargeCardZZZ")
	if !strings.Contains(out, "did you mean") && !strings.Contains(out, "refusing to guess") {
		t.Fatalf("P1 must refuse (suggest or candidates), got:\n%s", out)
	}
	out = run(1, "card", "CompletelyUnrelatedNameQQQ")
	if !strings.Contains(out, "no node matching") {
		t.Fatalf("P1b total miss must say so, got:\n%s", out)
	}

	// P2 — verify never fuzzes: the same near-name fails verification even
	// though card could fuzzy-resolve it.
	out = run(1, "verify", "ChargeCardX")
	if !strings.Contains(out, "missing-node") {
		t.Fatalf("P2 near-name must not verify, got:\n%s", out)
	}

	// P3 — a fabricated citation into a real file: out-of-range, loudly.
	out = run(1, "verify", "services/api/main.go:L9999")
	if !strings.Contains(out, "out-of-range") {
		t.Fatalf("P3 fabricated range must fail, got:\n%s", out)
	}

	// P4 — a fabricated file: missing-file, loudly.
	out = run(1, "verify", "services/api/imaginary.go:L1")
	if !strings.Contains(out, "missing-file") {
		t.Fatalf("P4 fabricated file must fail, got:\n%s", out)
	}

	// P5 — a real, exact citation verifies ok (the suite must not cry wolf:
	// defenses that fail good claims train agents to skip them).
	out = run(0, "verify", "BillingEngine.ChargeCard")
	if !strings.Contains(out, "ok") {
		t.Fatalf("P5 exact claim must verify, got:\n%s", out)
	}

	// P6 — a qualifier-guess that lands ambiguous refuses by default, and the
	// --fuzzy opt-in answer STAYS labeled fuzzy (an opt-in guess must never
	// masquerade as an exact resolution).
	out = run(1, "card", "acme.billing.ChargeCard")
	if !strings.Contains(out, "refusing to guess") {
		t.Fatalf("P6 ambiguous guess must refuse, got:\n%s", out)
	}
	out = run(0, "card", "acme.billing.ChargeCard", "--fuzzy")
	if !strings.Contains(out, "[resolved via fuzzy") {
		t.Fatalf("P6 --fuzzy answer must stay labeled, got:\n%s", out)
	}
}
