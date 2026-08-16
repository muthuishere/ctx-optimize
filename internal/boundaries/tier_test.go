package boundaries

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// isolated is hermetic() (boundaries_test.go: empty machine ladders) plus a
// fresh repo root to run the rule against.
func isolated(t *testing.T) string {
	t.Helper()
	hermetic(t)
	return t.TempDir()
}

// The defect (ADR 2026-08-15-authoring-loop-unenforced, D1): a rule that
// declares no tier AND ships no `verified` block was emitted as EXTRACTED — the
// one tier that asserts parsed certainty, handed out for providing no evidence
// at all. It now fails toward doubt like everything else here.
func TestUnmeasuredRuleDefaultsToAmbiguous(t *testing.T) {
	cases := []struct {
		name     string
		tier     string
		verified string
		want     string
	}{
		{"no tier, no evidence", "", "", schema.Ambiguous},
		{"no tier, measured", "", `{"expected":10,"matched":9}`, schema.Extracted},
		{"declared tier wins over silence", schema.Extracted, "", schema.Extracted},
		{"declared tier wins over evidence", schema.Inferred, `{"expected":1}`, schema.Inferred},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := isolated(t)
			r := Rule{ID: "unmeasured", Transport: "process.exec", Direction: "consumes", Tier: tc.tier}
			if tc.verified != "" {
				r.Verified = []byte(tc.verified)
			}
			if got := defaultedTier(&r); got != tc.want {
				t.Errorf("defaultedTier = %s, want %s", got, tc.want)
			}
			// And the same answer must reach the graph, not just the helper —
			// the tier travels on the edge's confidence.
			batch, err := Assemble(root, nil, []Rule{r},
				[]Hit{{Rule: "unmeasured", File: "a.go", Line: 3, Identifier: "git"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(batch.Edges) != 1 {
				t.Fatalf("want 1 edge, got %d", len(batch.Edges))
			}
			if got := batch.Edges[0].Confidence; got != tc.want {
				t.Errorf("emitted confidence = %s, want %s", got, tc.want)
			}
		})
	}
}

// The compatibility half, asserted rather than assumed: every shipped rule
// declares BOTH a tier and its measurement, so the defaulting rule above can
// never move one of them. If a future rule ships without either, this fails
// here rather than silently re-tiering the store.
func TestShippedRulesDeclareTierAndEvidence(t *testing.T) {
	root := isolated(t)
	rules, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	// 16 → 18 on 2026-08-16: `webstorage` split into webstorage-local,
	// webstorage-session and webstorage-cookie (ADR 26). The before/after port
	// diff this gate demands, run on two real repos with --force:
	//
	//   agentic-nexus  69 → 69 ports   1 storage.browser → 1 …browser.session
	//   volentis     1066 → 1067       47 storage.browser → 40 local + 7 session
	//                                  + 1 cookie
	//
	// Every existing port was RECLASSIFIED, none lost. The single new one is a
	// real site the old rule could not see: Cookies.set('lang', …) at
	// General.tsx:148, which no localStorage/sessionStorage shape matches.
	if len(rules) != 18 {
		t.Fatalf("shipped rule count moved: %d (was 18) — re-run the before/after port diff", len(rules))
	}
	for i := range rules {
		r := &rules[i]
		if r.Tier == "" {
			t.Errorf("shipped rule %q declares no tier", r.ID)
		}
		if len(r.Verified) == 0 {
			t.Errorf("shipped rule %q ships no verified block — \"measured, or it does not ship\"", r.ID)
		}
		if got := defaultedTier(r); got != r.Tier {
			t.Errorf("shipped rule %q: defaulting changed its tier %s → %s", r.ID, r.Tier, got)
		}
	}
	if un := Unmeasured(rules); len(un) != 0 {
		t.Errorf("unmeasured shipped rules: %v", un)
	}
}

// `boundaries verify` must report the tier the engine actually emits at. A
// verb whose job is holding a rule to its evidence cannot be the place that
// prints EXTRACTED for a rule the engine demotes.
func TestVerifyReportsTheDemotedTier(t *testing.T) {
	root := isolated(t)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Rule{ID: "unmeasured", Transport: "process.exec", Direction: "consumes"}
	res := verifyRule(root, &r, nil)
	if res.Tier != schema.Ambiguous {
		t.Errorf("verify reports tier %s for an unmeasured rule, want %s", res.Tier, schema.Ambiguous)
	}
	if res.Status != StatusUnverifiable {
		t.Errorf("status = %s, want %s", res.Status, StatusUnverifiable)
	}
}
