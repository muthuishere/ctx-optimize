# ADR — `verify`: deterministic citation checking; ambiguity-aware resolution; the two-sided ladder

Status: APPROVED (maintainer 2026-07-17: "this will be a high issue, if
this will fix it will be wonderful"). Directed 2026-07-16: "sometimes the
model gets too hallucinated, need some way to get defensive / self-find a
reason", "instead of reading code it was stopping and doing bad", and
"sometimes it started using [the store] instead of regular grep".

Predecessor evidence (graphify audit, 2026-07-17, graphifyread repo):
graphify resolves exact → prefix → substring and its `explain` silently
takes `matches[0]` — a near-miss (`PayInvoice` vs `PayInvoiceRetry`)
answers about the WRONG symbol with zero signal (serve.py:687-752,
cli.py:724). Its only verification is the opt-in `reflect` fingerprint
overlay: file-level, hint-only, never blocks, covers only previously-cited
nodes. Its anti-hallucination story is prompt-level rules — the exact
mechanism we've watched models over- and under-apply. Copy: per-edge
confidence, provenance-in-output, `path`'s ambiguity warning. Reject:
silent nearest-match answers; unenforced grounding.

## Context

Two observed hallucination modes when agents answer from the store:

1. **Invented or drifted citations.** An agent presents `auth.go L40-L80`
   shaped exactly like a real hit — but the symbol isn't there (fabricated),
   or was there at gather time and the file has since changed (drift).
   Nothing today can check a citation mechanically.
2. **Stopping instead of reading.** The skill's gate ("cite it directly,
   do NOT re-verify in source") is deliberately aggressive to kill
   grep-first habits — but models over-generalize it into "reading files is
   forbidden", stop at a thin store answer, and fill the gap from priors.
   The gate was aimed at *blind grep*, not at reading code the store
   already located.
3. **Over-using the store where grep is the right tool** (maintainer:
   "sometimes the LLM started using it instead of regular grep"). The
   store doesn't index literal strings, config values, comments, member
   fields, or build files — but the gate's absolutism makes models force
   `query` there anyway and THRASH: miss → rephrase → miss → answer from
   priors. The gate needs an explicit "wrong tool, switch" rule, not just
   an exceptions footnote.

A third, tool-manufactured mode: fuzzy resolution. `card`/`explain`/
`affected` resolve id > label > fuzzy tokens, so asking about `PayInvoice`
can silently answer about `PayInvoiceRetry` — confident, cited, wrong
subject.

All fixes below are deterministic — no LLM in the binary, ever.

## Decision 1 — `ctx-optimize verify` (the defensive verb)

```
ctx-optimize verify "<node-id | exact-label | file:Lstart-Lend>" [--json]
```

Checks that a claimed citation HOLDS, mechanically:

- the node exists in the store (by id or EXACT label — verify never
  fuzzes);
- its recorded source file exists on disk (repo-relative to the scope);
- the recorded line range is within the file's current bounds;
- drift: whether the file changed since gather, from the provenance the
  store already records (git HEAD at add time) — file differs between
  store-HEAD and worktree ⇒ verdict `drifted` (a `sync` re-anchors it);
  non-git repos report `drift: unknown` rather than a false `ok`.

Output: verdict per claim — `ok | drifted | missing-node | missing-file |
out-of-range` — with the evidence (expected vs found). Exit 0 only when all
claims are `ok`; 1 otherwise. Accepts multiple claims in one call.

Implementation note: drift rides the EXISTING git provenance
(freshness source.json) instead of new per-file hashes at gather — the
gather hot path stays untouched, so the bench gather gate (≤+5%) and the
golden extraction snapshots carry zero risk from this change.

## Decision 2 — ambiguity-aware resolution (safe by DEFAULT, no flag to forget)

graphify's lesson: an opt-in strictness flag rots exactly like prompt
rules do. So the safe behavior is the default:

- Every resolving verb (`card`, `explain`, `affected`, `path`,
  `change-plan`) emits `resolved_via: exact-id | exact-label | fuzzy` and
  the resolved node id — in --json AND the human output header.
- Fuzzy resolution with a CLEAR winner (runner-up scores below a gap
  threshold) → answer, banner loud.
- Fuzzy resolution with a CLOSE runner-up → REFUSE and return ranked
  "did you mean" candidates instead of answering about a guess. `--fuzzy`
  opts into taking the nearest anyway (for scripts that accept the risk).
- Gap threshold starts at graphify-`path`'s 10% relative gap; tuned
  against the measured refusal rate below before merge.

## Decision 3 — the two-sided ladder (skill discipline rebalance)

The absolutist gate is replaced by a TOOL-CHOICE rule plus a ladder the
agent descends instead of stopping. Both failure directions are named.

**Pick the tool by the QUESTION, before the first call:**

| Question shape | Tool |
|---|---|
| symbols, structure, callers, impact, architecture, "how does X work" | store verbs (query/card/change-plan/affected/path/hubs) |
| exact literal strings, every occurrence, config VALUES, comments, member fields, build files, error-message text | **grep directly — the store does not index these; using query here is the wrong tool, say so and grep** |

**The ladder (descend, never stop):**

1. Right-tool store verb first.
2. Before presenting citations a human will act on: `verify` them; a
   failed verify means re-query or `sync` — never rephrase the claim.
3. **When the answer depends on behavior — logic, edge cases, actual
   values — READ the cited range.** Opening the file at a store-provided
   `file:line` is not a gate violation; it is the point of the location.
   The gate forbids *blind* grep-and-browse, not reading what the store
   located.
4. **Two store misses = switch tools, not words.** A third rephrase is
   thrash; go to hubs/explain-on-a-neighbor, or declare the grep lane and
   grep. Log the miss (`save-result --outcome dead_end`).
5. Still nothing → abstain, stating exactly what's missing and which
   gather lane would fix it. The one forbidden move is stopping silently
   or padding from priors.

Skill changes: SKILL.md gate + answering discipline + query-craft sections
rewritten around the table and ladder; `verify` added to the intent router
("about to hand a citation to a human / does this claim still hold") and
activation-routing.xml (new route + defaults rule); failed verifies wire
into the save-result loop (outcome dead_end with the verdict as evidence)
so `reflect` surfaces recurring drift — the "self-find a reason" trail.
The hook context (`hook-context`) gains one line of the tool-choice rule
so the routing survives even when the skill isn't loaded.

## Measured merge gates (numbers, not assertions)

- **Ambiguity-refusal rate** on both golden corpora's 20-question
  scoreboards: refusals must be RARE (target: 0 refusals on the current
  question sets — they use exact labels or clear winners; any refusal is
  investigated, the threshold tuned, and the final rate recorded in the
  CHANGELOG).
- **Golden score floors hold** (16.5/20 both corpora) and **bench gates
  hold** (gather ≤+5%, query ≤+10%, tokens ≤+20%) — resolution changes
  ride the same never-regress net as everything else.
- **verify latency** measured and recorded (expected: store-load dominated,
  same order as `card` ≈ sub-ms warm).

## Consequences

- Hooks/CI can enforce grounding: `verify` any citation an agent emits.
- Verify is read-only, works offline, and adds no network or model — the
  deterministic contract is untouched.
- The gather hot path is untouched (drift via existing git provenance) —
  zero risk to extraction snapshots and gather ceilings.
- Ambiguous fuzzy asks gain one round-trip (candidates → pick) ONLY in the
  close-runner-up case; the wrong-symbol chain it replaces costs far more.
