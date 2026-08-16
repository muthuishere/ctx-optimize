package boundaries

// Tier defaulting (ADR 2026-08-15-authoring-loop-unenforced, D1, option b).
//
// Measured before this change: a rule with NO `verified` block loaded without
// complaint, and an omitted `tier` defaulted to EXTRACTED — so the one tier
// that asserts parsed certainty was what a rule got for providing no evidence
// at all. Every other confidence decision in this product fails toward doubt: a
// computed identifier is AMBIGUOUS, a call resolved by unique name is INFERRED,
// a name defined twice is AMBIGUOUS and filtered out of traversals. This one
// failed toward certainty.
//
// So: an UNMEASURED rule — one that declares no tier AND ships no `verified`
// block — is AMBIGUOUS. A rule that carries its measurement but omits the tier
// keeps the old default, EXTRACTED, which is why all 16 shipped rules (every
// one of which declares BOTH) are unaffected. The cost lands exactly where the
// doctrine says it should: on a rule authored against the old permissiveness
// that never measured anything.
//
// Why not reject an unmeasured rule at load: `boundaries verify` already
// reports it as `unverifiable`, naming the rule and saying a rule without its
// measurement is invalid by definition. Refusing to load would take a working
// draft rule away from an author mid-loop, which is when they most need to see
// what it captures. Demoting its confidence keeps the rule runnable and keeps
// the graph honest about what it is — the same trade the AMBIGUOUS shortlist
// makes for an over-resolved call. `Load` has no writer to warn on (it runs
// deep inside the extractor walk, and a library that prints to stderr would
// corrupt every `--json` run), so `boundaries verify` stays the reporting
// surface.

import "github.com/muthuishere/ctx-optimize/internal/schema"

// defaultedTier resolves the tier a rule's sites are emitted at. Declared tier
// wins; otherwise evidence decides.
func defaultedTier(r *Rule) string {
	if r.Tier != "" {
		return r.Tier
	}
	if len(r.Verified) == 0 {
		return schema.Ambiguous
	}
	return schema.Extracted
}

// Unmeasured reports the ids of rules that ship no `verified` block, in load
// order (Load sorts by id). It is the list `boundaries verify` counts as
// unverifiable, exposed for any caller that wants the names without re-running
// the whole verification.
func Unmeasured(rules []Rule) []string {
	var out []string
	for i := range rules {
		if len(rules[i].Verified) == 0 {
			out = append(out, rules[i].ID)
		}
	}
	return out
}
