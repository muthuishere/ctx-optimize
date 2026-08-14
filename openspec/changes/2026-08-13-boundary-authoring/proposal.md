# ADR 2 — generating boundaries: measured or invalid

Status: DRAFT — owner review pending 2026-08-13. No product code until agreed.
Part 2 of 3. Depends on ADR 1 (`2026-08-13-boundary-model-and-defaults`) for
the `port` kind, `boundaries.json`, and the `search` verb. ADR 3 renders it.

## Context — why generation must be adversarial to itself

The spike that proved the pipeline also proved how it lies:

- **recall failure:** a rule matched 16 of **137** real ui→api call sites
  (12%) — every call routes through one wrapper (`apps/ui/src/lib/http.ts`),
  and a literal-argument regex cannot see through it. The tool reported
  nothing wrong.
- **precision failure:** a vendored corpus
  (`benchmarks/corpus-flask/docs/conf.py`) was reported as production egress.

Both were caught by accident. A boundary graph that is silently 12% complete
is worse than no graph — agents and humans will trust it. So: **a rule without
its measurement is invalid by definition.**

## D1 — every rule carries evidence; the tier IS the measurement

```json
"verified": {
  "at": "2026-08-13",
  "ground_truth": { "tool": "ctx-optimize search",
                    "cmd": "ctx-optimize search 'exec\\.Command\\(' --ext .go -c" },
  "expected": 52, "matched": 50, "recall": 0.96,
  "sampled": 10, "confirmed": 10, "precision": 1.00,
  "tier": "EXTRACTED",
  "known_misses": ["exec.CommandContext(ctx, binVar) — binary is a variable"]
}
```

- **recall** = matched ÷ ground truth (catches the 16-vs-137 class)
- **precision** = confirmed ÷ sampled, each hit checked at its `file:line`
  (catches the vendored-corpus class); `ctx-optimize verify` is exactly this
- tier is DERIVED, never asserted:
  `≥0.95 → EXTRACTED · 0.70–0.95 → INFERRED · <0.70 → AMBIGUOUS or reject`
- `known_misses` is **mandatory** when recall < 1.0 — silence that looks like
  completeness is the one failure that makes a graph lie.

## D2 — ground truth must be independent of the rule, and assume no tooling

| rank | source | independent because |
|---|---|---|
| 1 | framework registry — swagger, `go list`, router table, migrations | authoritative by construction |
| 2 | our own verbs — `nodes`, `edges`, `card`, `affected`, `verify` | tree-sitter-derived, not the rule's regex; always present; fastest |
| 3 | `ctx-optimize search` (ADR 1 D4) | raw count vs capture logic; cross-OS incl. Windows; same file set as the extractor |

External `rg`/`grep` are optional cross-checks only — nothing depends on them.
The `verified.ground_truth` records the tool, so any machine reproduces the
number or reports the substitution.

## D3 — the standing check: `ctx-optimize boundaries verify`

Re-runs each rule's ground truth and reports drift:

```
process-exec   recall 0.96 → 0.71   ⚠ 14 new exec sites unmatched
http-egress    recall 0.98 → 0.98   ok
```

CI-runnable. Governed like the golden net: numbers only move up; lowering a
floor is a reviewed diff. This is also how ADR 1's shipped defaults are held
to their own `verified` blocks on the corpus sweep.

## D4 — the `boundaries-author` agent skill

The generator is the host agent, per doctrine: the model runs ONCE at
authoring time; its output is reviewed config committed beside the code. The
binary stays deterministic and model-free.

The loop, fixed — every step required:

```
1 SURVEY    from the store, not the filesystem: deps, nodes --kind,
            edges --relation; the gap between "kinds present" and
            "kinds connected" is the work list
2 PROPOSE   one rule, smallest useful scope
3 GROUND    independent count (D2 ranking)
4 RUN       apply the rule
5 MEASURE   recall AND precision (D1)
6 ITERATE   fix what the measurement exposes — the 137 case was solved by
            matching the wrapper's exported names, found in step 6, not
            by a cleverer first regex
7 TIER      derive from the measurement
8 WRITE     rule + verified block; no evidence, no rule
```

Hard rules: emits **data, never code** (the adapter door needs written
justification); secrets by NAME only, `sensitive` flag on
`KEY|TOKEN|SECRET|PASSWORD`; no network during authoring; vendored trees
excluded by default; a rule that cannot ride the engine's walk is declared,
not smuggled in as a second pass.

Definition of done: every rule verified · `boundaries verify` passes ·
`nodes --kind port` returns what the survey predicted · the diff is JSON a
reviewer can read.

## D5 — graduation path

repo rule → proves out across repos (its `verified` blocks keep passing on
the corpus sweep) → promoted **verbatim** into ADR 1's embedded defaults, with
its evidence. Adapters are the frontier; defaults are the settled core; the
promotion is a reviewed diff, not a rewrite.

## Spikes

| # | question |
|---|---|
| A1 | the loop end-to-end on reqsume: does it reach recall ≥ 0.9 on ui→api (137) without human hints? |
| A2 | ground-truth source agreement: rank 1 vs 2 vs 3 on the same rule — do they converge? |
| A3 | `boundaries verify` drift detection: mutate the repo, confirm the ⚠ fires |
| A4 | the skill on a cold repo (cljgo): what does it produce with zero prior knowledge, and what does it honestly refuse to claim? |
