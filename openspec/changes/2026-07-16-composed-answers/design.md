# Design evidence — review round 3 findings + spike results (2026-07-16)

Proposals in both 2026-07-16 change dirs are FROZEN (maintainer). This file
records the iterative-review findings and the de-risking spikes that ran
after the freeze, so implementation starts from measured fact. Nothing here
alters a decision; it grounds them.

## The golden net exists (prerequisite for every step)

`internal/golden/` — hermetic fixtures (a multi-module config repo with a
multi-path src/+tests/ .NET module; a plain csproj/sln repo) pinned as exact
snapshots + named landmarks + query-ranking goldens, in every `go test ./...`.
Corpus tier (env-gated; `.github/workflows/golden.yml` shallow-clones pins):
linux v6.9 `block/` and Newtonsoft.Json 13.0.3 as a REAL multi-path config
repo. Any feature that shifts extraction or ranking on these fails with a
diff before it ships. Validated green against real clones locally.

## Spike results (all measured on real stores, 2026-07-16)

### S-A: `tests-for` derived view — CONFIRMED, no persisted edge needed

Prototype = `affected <symbol> --json` + test-convention filter on source
paths, request-time only:

- This repo, `EnsureGlobalPointer`: 12 impacts → 5 test facts
  (TestGlobalPointerLifecycle, TestGlobalPointerCreatesMissingFile via calls;
  3 test FILES via co-change) in **55ms** end-to-end.
- Newtonsoft (10,131-node store), `JsonConvert.SerializeObject`:
  correct test class surfaced in **103ms**.
- Finding: co-change surfaces test *files* alongside call-edge test *functions*
  — maps directly onto the spec's confidence tiers (calls-EXTRACTED/INFERRED
  strong, co_changed_with weak-evidence). The A1 confidence block should
  label these tiers, not merge them.

### S-B: yamlwalk vs real GitHub Actions — CONFIRMED, Move 2 core is viable with zero new deps

Ran the existing line-walker over `.github/workflows/ci.yml` (real file):
extracted workflow name, `on:` trigger presence, both jobs (`test`,
`dashboard`), and all **8 step run commands** verbatim (`go vet ./...` …
`npm test`). Two bonus findings:

- YAML's notorious `on:`-coerced-to-boolean problem does not exist here —
  no YAML library, the walker sees the literal key. Accidental advantage of
  the stdlib rule.
- Of the 8 run commands, `npm test` normalizes exactly onto the existing
  `package.json::task:test` node shape; `go test ./...` etc. are raw
  commands with no task node — confirming the spec's EXTRACTED (exact match)
  vs INFERRED (normalized) vs no-link split is the right contract.

### S-C: multi-path contract at real scale — measured

Newtonsoft 13.0.3 as `{"paths": ["Src/Newtonsoft.Json",
"Src/Newtonsoft.Json.Tests"]}`: **344 cross-split test→source calls edges**
(of 5,224 total calls). Golden floor set conservatively at 50.

## Bench baselines for step 0 (feed `task bench-extract`)

| Corpus | Nodes | Edges | Gather (init+add, wall) |
|---|---|---|---|
| linux v6.9 block/ | 8,163 | 12,007 | 1.0s |
| Newtonsoft.Json 13.0.3 (multi-path) | 10,131 | 19,194 | 1.7s |
| this repo | ~1.9k | ~4.8k | <1s |
| query avg (this repo, live metrics) | — | — | 7.0ms (n=92); card 0.6ms (n=91) |

## Review round 3 findings (for implementation, not spec change)

1. **JSON envelope inconsistency** (found by the golden suite's fail-fast):
   scoped queries answer `{"result": {...}, "scope": ...}`, unscoped answer
   the flat object. A2's composed verbs must define ONE envelope and the
   existing verbs should converge on it — agents parse this surface.
2. **Store keying asymmetry**: single-path modules key by PATH
   (`goldenmm/services/api`), multi-path by NAME (`goldenmm/Billing`). Now
   pinned by the golden suite, so any unification is a deliberate,
   diff-reviewed change; until then the skill assets should state it.
3. **Newtonsoft gather emits a root store + module store** — same shape as
   monorepos; composed verbs must aggregate across keys the way `storeKeys`
   walking does, not assume one store per repo.
4. Corpus specs must stay in lockstep with workflow clone refs (both carry
   the pin; a mismatch skips loudly).

## Design decision — the main idea, sharpened (maintainer, 2026-07-16)

"The main idea is: find X — DIFFERENT information — across a multi-module
repo, in one search." Verified against the multimod fixture: root-federated
query already retrieves across modules and facets (found ChargeCard + its
test across the src/tests split; found `build` tasks in two modules). What
fails the idea is PRESENTATION, not retrieval:

1. Hits carry no module attribution — the first fact a monorepo answer needs.
2. One flat ranked list — facets (code / tests / tasks / deps / routes /
   config) interleave and compete; a strong code match starves the single
   route/dep/task fact out of the budget.

Next implementation slice (before `trace`): **faceted federated query** —
root-scope output grouped by module, hits tagged and sectioned by facet,
with per-facet slot guarantees (the dashboard's producer-sample fairness,
applied to query). Output-shaping only, no new indexing. Expected scoreboard
effect: N17/N18/N19 (test-noise ranking gaps) flip when tests become their
own section — score rises, floors ratchet, goldens regenerate as a reviewed
diff (query top-k is a pinned contract).
