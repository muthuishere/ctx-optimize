# ADR — composed one-call answers, confidence reporting, review-diff (external proposal, reviewed & adopted with corrections)

Status: RESEARCH v1 — 2026-07-16. Source: an external LLM proposal supplied by
the maintainer, fact-checked against this repo and reconciled with the
companion ADR `2026-07-16-question-shaped-extraction/`. Review verdict:
grounded (every cited number traces to proof/RESULTS.md, docs/CRITIQUE.md, or
live metrics — query avg 7.0ms n=92, card 0.6ms n=91, verified at review
time); core ideas adopted; thresholds and sequencing corrected below.

## Positioning (adopted verbatim, it matches our own kill-test conclusions)

The value is NOT "a faster grep." It is **correct architecture answers in
fewer tool turns** — impact analysis, onboarding, legacy code — and making
cheaper agents behave like stronger ones (Devin 2.3M→412k tokens, −82%;
the competence-equalizer finding, proof/RESULTS.md). Token savings stay a
secondary, qualified claim: ~30–45% on grep-hostile/legacy code; ~0% or
negative on modern well-named repos (docs/CRITIQUE.md). Turns, wall time,
and correctness lead.

## Adopted moves (answer-side; composition, not new indexing)

### A1 — confidence & completeness block on every answer

Every query/card/affected/path answer gains a footer built from metadata the
engine already holds: extracted vs inferred counts, unresolved/dropped
dynamic-dispatch sites, git co-change evidence, store freshness (head sha),
and missing-coverage notes (e.g. "no route pack matched this framework").
Output-only change; negligible cost; directly strengthens the measured
correctness moat (the q5 wrong-gatekeeper case). Ships FIRST.

### A2 — composed one-call verbs: `trace`, `change-plan`

- `ctx-optimize trace "<concept>"` — entry points → call chain → routes/
  deps/config touched, one bounded answer (compose query+card+path+hubs).
- `ctx-optimize change-plan "<symbol>"` — affected + tested_by + co-change +
  module/community context, shaped as "what to touch, what to run, what to
  watch".

Rationale: agents currently spend turns stitching primitives; turns are the
measured value axis. Targets (adopted): ≥30% fewer tool calls on the proof
question set; default output ≤3,500 tokens; p95 ≤150ms medium repo / ≤400ms
federated. `why-coupled` is deferred (low frequency) — stretch, not v1.

### A3 — `review-diff`: the diff-aware change briefing

Changed decls → inbound callers/reverse impact → affected modules/communities
→ relevant tests → routes/deps/config/deploy surfaces → co-changed files →
confidence block. Bounded traversal from changed files only. Validation:
replay recall against historical PRs (cheap: git log + affected).

**Corrected dependency (twice-corrected 2026-07-16):** the "relevant tests"
row needs the request-time TEST CLASSIFIER shared with `tests-for` (companion
ADR Move 3 as revised) — NOT a persisted `tested_by` edge. Second external
review proved, and live verification confirmed, that `affected` already
surfaces test callers via existing incoming calls edges; the classifier just
labels them. The "deployment surfaces" row is only as rich as the dev-env/CI
lanes (companion ADR Moves 1–2) — it ships degraded without them and improves
as lanes land.

### A4 — routing honesty (skill-side, not engine)

Literal enumeration → rg; symbol understanding → card; lifecycle → trace;
change impact → change-plan/review-diff; simple modern-code lookup →
whichever is cheaper. Correction to the source proposal: this mostly EXISTS
(the skill's grep lanes, THE GATE's three exceptions); the adoption is a
refinement of the routing table + the new composed verbs as first-class
routes, not new machinery.

## Reconciliation with 2026-07-16-question-shaped-extraction

The two ADRs are complementary halves, not rivals:

- THIS one is **answer-side**: compose what the store already knows into
  one-call, confidence-labeled answers.
- THAT one is **fact-side**: new deterministic lanes (dev-env, CI/CD,
  tested_by, schema) so those answers cover the operational axis nobody
  (including graphify) serves.

The unified execution plan for BOTH ADRs lives in `./tasks.md` (revised after
the second external review): fact-pack contract + bench harness first, then
the RUNBOOK vertical slice, compose/Dockerfile, `tests-for` as a derived
view, GitHub Actions with normalized task-linking, schema topology behind
per-framework accuracy spikes, and question-shaped views last — with A1
(confidence block) and A2 (trace/change-plan) interleaved where their inputs
exist. Both binding rules from the companion ADR (single fact-pack contract;
executable cumulative bench gates) govern every step.

## Performance constitution (adopted as REGRESSION gates, with baselines)

Adopted table, corrected from aspiration to regression-gate by recording the
measured baseline beside each budget (several budgets are already beaten by
7–10×; the gate is "don't lose it", not "reach it"):

| Operation | Budget | Measured baseline (2026-07-16) |
|---|---|---|
| query/card, medium repo | p95 ≤ 50ms | avg 7.0ms / 0.6ms (n=92/91, live metrics) |
| query/card, large federation | p95 ≤ 150ms | to measure (chromium corpus) |
| composed trace, medium | p95 ≤ 150ms | n/a (new) |
| composed trace, federated | p95 ≤ 400ms | n/a (new) |
| 5-file refresh | p95 ≤ 500ms/module | to measure |
| monorepo incremental refresh | ≤ 2s | to measure |
| full gather | ≥ 1,000 files/s | ~8,000 files/s (4k files ≈ 0.5s) |
| query regression from any new feature | < 20% | gate |
| code-only gather regression | < 10% | gate |
| default composed output | ≤ 3,500 tokens | query default budget 2,000 |

Constitution constants (all already house rules, restated): no embeddings/
vector DB; no LLM in the binary; no mandatory daemon or MCP; precompute
compact facts + adjacency, generate task answers on demand; producers
optional and independently replaceable.

**Corrected acceptance rule:** the source proposal's "reject <5 correctness
points or <30% turn reduction" is the right spirit with arbitrary numbers —
measuring correctness points per feature can cost more than the feature. The
enforceable form is the companion ADR's binding rule 2: every step ships with
its measured A/B (turns, tokens, wall, correctness where the class allows) in
proof/, and its tasks.md states the target it is accountable to.

## Not now (adopted, one deletion from our own list)

Generic chat UI · autonomous codegen · embedding search · always-on watcher
· more dashboard polish · unbounded history/log ingestion · build-time LLM
wiki — all rejected, unchanged. ALSO adopted against the companion ADR:
"broad language expansion without demonstrated demand" — the companion's
"grow the addable-by-name grammar list" cheap-win is DROPPED until a user
asks for a specific language.
