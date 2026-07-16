# Unified execution plan — composed-answers + question-shaped-extraction

One plan for both 2026-07-16 ADRs, revised after the second external review
(accepted corrections: tests-for as derived view, single fact-pack contract,
executable cumulative bench gates). Every step ships alone behind `task ci`,
records its bench run in `proof/bench/`, and its A/B (turns/tokens/
correctness where the class allows) in `proof/`.

## Step 0 — bench harness (the gate everything else runs under)

- `task bench-extract`: cold gather p50/p95 (≥10 runs), 5-file incremental
  refresh, peak RSS, store bytes/nodes/edges, query/card p50/p95; record =
  corpus+commit, files+bytes, machine+Go version, baseline vs candidate.
- Corpora: this repo + the chromium subset already gathered.
- First run establishes the baselines the gates diff against
  (measured 2026-07-16: query avg 7.0ms n=92, card 0.6ms n=91, full gather
  ~8k files/s).
- Deliverable: harness + first committed baseline record. No product change.

## Step 1 — fact-pack contract v1 (design first, no new recognizers)

- One versioned schema: `{"schema":1, "name", "capabilities":[...],
  "rules":[...]}` covering node identity templates, node emission, edge
  emission (direction, cardinality, unresolved-ref policy, collisions,
  cross-file linking), confidence/provenance, resource ceilings, fail-closed
  validation.
- One CLI: `packs add <name|url>` / `packs list [--capability X]` /
  `packs remove`. routes/manifests/languages verbs stay as aliases into the
  same registry (shipped surface, never breaks).
- Core recognizers share the EMIT contract, parse however they like.
- Deliverable: design.md + schema + registry + validation tests. Gate: zero
  regression (bench diff vs step 0 baseline).

## Step 2 — vertical slice: RUNBOOK from task facts

- Recognizers: Taskfile(.{env}).yml, Makefile, justfile → `task` nodes
  (command line + desc), joining the existing npm-scripts/gradle task kind.
- Surface BOTH ways: wiki `RUNBOOK.md` + `ctx-optimize runbook` (same render).
  Answers "how do I build/test/run this repo" with file:line citations.
- A/B: the runbook question class vs grep baseline, this repo + one polyglot
  repo. Bench: cumulative gate.

## Step 3 — compose + Dockerfile (extend RUNBOOK, no new surface)

- compose.yaml → `service` nodes (image, ports, env key NAMES, `depends_on`
  edges); Dockerfile → `image` node (FROM, EXPOSE, entrypoint);
  .env.example → `config` keys with `declared_in` provenance only (no
  cross-layer resolution in v1).
- RUNBOOK gains services/ports/env sections. Same gates.

## Step 4 — `tests-for` derived view + confidence block (A1 lands here)

- `ctx-optimize tests-for <symbol>`: filter `affected`'s incoming callers to
  recognized test declarations (naming/path conventions), bounded depth,
  request-time only — NO persisted edge (falsified premise recorded in the
  companion ADR).
- Ships together with A1: the confidence/completeness footer (extracted vs
  inferred counts, unresolved dispatch, freshness sha) on tests-for, affected,
  query, card.
- A/B: "which tests do I run for this change" vs grep. Gate: query p95
  regression ≤10%.

## Step 5 — composed verbs: `trace` + `change-plan` (A2)

- Compose query/card/path/hubs (+ tests-for, + runbook facts where present)
  into one bounded answer; default output ≤3,500 tokens; p95 ≤150ms medium /
  ≤400ms federated.
- Skill routing table update rides along (A4): literal→rg, symbol→card,
  lifecycle→trace, impact→change-plan.
- A/B target: ≥30% fewer tool calls on the proof question set.

## Step 6 — GitHub Actions lane + `review-diff` (A3)

- Workflows → workflow/job nodes; step→task linking with normalization
  semantics (exact=EXTRACTED, normalized=INFERRED, `${{ }}`/matrix=skip;
  reusable workflows out of v1).
- Then `review-diff`: changed decls → callers/impact → tests (via tests-for
  classifier) → routes/deps/config/deploy surfaces → co-change → confidence
  block. Bounded traversal from changed files; p95 ≤300ms for ≤50 files.
- Validation: recall replay against this repo's historical PRs.
- Wiki PIPELINE.md once workflow facts prove out.

## Step 7 — schema topology (accuracy-spiked, one framework at a time)

- SQL FK `references` edges first (grammar already parsed; relation dropped
  today).
- Migration ordering: per-framework accuracy spike BEFORE nodes ship (Django
  deps ≠ Rails timestamps ≠ Flyway versions); start with the first real user
  repo's framework. ORM schemas as packs via the contract.
- Live DB introspection stays adapter-only (psql/pg_dump shell-out example
  asset in the skill).
- Wiki SCHEMA.md + TESTING.md once facts are sufficiently complete.

## Deferred (explicitly, with reasons)

- Persisted `tested_by` — only if a benchmark proves the derived view
  insufficient.
- `why-coupled` — low frequency.
- Rationale nodes — no proven question class yet.
- Grammar-list growth — no demonstrated demand.
- GitLab/Jenkins/CircleCI — packs via the contract, when asked for.
