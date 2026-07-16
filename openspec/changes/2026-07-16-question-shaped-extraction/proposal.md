# ADR — question-shaped extraction: answer "how do I run / test / deploy / store data" without an LLM

Status: RESEARCH v1 — maintainer-requested 2026-07-16 ("how ci cd setup, tests
are setup, how to setup local dev environment, how things are configured, how
the actual schema in db is … make the answer coming without llm itself so llm
is just cherry on cake … we dont want to go away from core, at the same time
we want to improve iteratively"). No implementation started.

## Context — what the store answers today, and what users actually ask

Current deterministic surface (verified against the binary + `internal/extract/`
on 2026-07-16):

- **Code**: 12 embedded languages (go, python, js, ts, tsx, java, c, cpp,
  csharp, rust, zig, sql) + 16 addable by name + any tree-sitter grammar as a
  pack. Decls, contains, imports, module-wide call resolution.
- **SQL**: DDL declarations (`create_table→table`, views, functions, indexes,
  schemas, types, triggers, sequences) — `internal/extract/code/langs.go:169`.
- **Docs**: markdown/txt sections with stable slugs.
- **Routes**: 10 core recognizers (fastapi, flask, express, nestjs, angular,
  react-router, vue-router, openapi, drupal, ingress) + packs.
- **Manifests**: 7 core recognizers (npm, maven, csproj, sln, go.mod, gradle,
  k8s) + packs; npm scripts → `task` nodes; k8s images → `image` nodes.
- **Git co-change** edges; deterministic communities (degree-penalized label
  propagation, zero randomness); hubs; an LLM-free wiki (index/hub/file pages).

But the highest-frequency **onboarding questions** have no lane at all:

| Question | Covered today? |
|---|---|
| "How is CI set up? What runs on a PR? On a tag?" | ❌ `.github/workflows/` not parsed |
| "How do I run this locally?" | ❌ Taskfile/Makefile/justfile/compose/devcontainer/.env not parsed (only npm scripts) |
| "How are tests set up? Which tests cover X?" | ❌ no `tested_by`; test→source `calls` edges EXIST but are not surfaced as topology |
| "What is the actual DB schema?" | ◐ DDL decls parsed, but no FK edges, no migrations ordering, no ORM schemas, no schema page |
| "How are things configured?" | ◐ config files indexed as documents; `.env.example`/compose env not modeled |

The agent falls back to grep-and-read for exactly these — the cost the store
exists to kill.

## Competitive fact-check (graphify 0.9.x, inventoried from source 2026-07-16)

Reproducibility (review tightening): inventory taken from the local clone at
`~/muthu/gitworkspace/graphifyread`, commit `f16ba8a` (2026-07-11). Key
source paths: `graphify/extractors/__init__.py` (language registry),
`graphify/detect.py` (extension map), `graphify/extractors/engine.py`,
`graphify/cluster.py` (Leiden seed 42), `graphify/paths.py`
(`_TEST_FILENAME_PATTERNS` — naming-heuristic scoping only), CHANGELOG.md
("Dockerfile/Makefile … remains a follow-up").

Where they are ahead, deterministically: ~40 languages (vs our 12+16+packs),
Terraform/HCL, SQL **FK/JOIN edges**, live Postgres introspection (opt-in),
rationale nodes (`# NOTE/WHY/HACK` comments + ADR `doc_ref` edges), indirect
dispatch (callback-by-name) resolution, MCP-config ingest. Their media breadth
(PDF/images/video/Office) is **LLM-dependent** — off-limits for our core and
explicitly not envied.

Where the "run/test/deploy" axis stands for them: **absent too.** No CI/CD, no
Docker/compose, no Kubernetes (we HAVE k8s), no Makefile/Taskfile (their
CHANGELOG marks Dockerfile/Makefile "a follow-up"), no migrations model, and
test-awareness is only a naming heuristic used to scope call resolution — no
test topology. `graphify prs` reads live GitHub PR/CI *status* via `gh`, but
never parses pipeline definitions.

**Conclusion: the operational axis is an open lane.** Nobody answers it from a
graph today, every input is a file already in the repo, and every parse is
deterministic. This is differentiation that deepens the core instead of
leaving it.

## Decision — five moves, each shippable alone, ordered by value-per-effort

The doctrine stays: core recognizers for the ubiquitous shapes, packs for the
tail, adapters for anything live. No LLM, no DB drivers, no network in the
binary — ever. The LLM's only job is narrating facts the store already holds
("cherry on cake").

### Binding rule 1 — ONE versioned fact-pack contract, not per-lane doors (REVISED after external review, 2026-07-16)

Extensibility is not a follow-up — but the first draft of this rule
(`ctx-optimize <lane> add` per lane) was rejected in review as CLI sprawl:
`ci add`, `services add`, `schema add`, `tests add`… would multiply command
surfaces and implementations. Revised design:

- **One pack surface**: `ctx-optimize packs add <name|url>` ·
  `packs list [--capability ci]` · `packs remove <name>`. A pack DECLARES its
  capabilities instead of belonging to a lane:
  `{"schema": 1, "name": "github-actions", "capabilities": ["workflow",
  "job", "task-link"], "rules": [...]}`.
- **The contract covers what widening `validEmits` cannot**: input matching,
  parser mechanism, node identity templates, node emission, EDGE emission
  (direction, cardinality, unresolved-reference policy, cross-file linking,
  collision behavior), confidence/provenance stamping, resource ceilings,
  schema version, fail-closed validation. Today's manifest pack validation
  recognizes only formats/paths/`{dependency, task}` — insufficient for
  edges; the contract is a design task of its own (step 1 of the plan).
- **Core recognizers and packs share the EMIT contract, not the parse
  mechanism.** A hand-written single-pass Dockerfile recognizer is faster and
  safer than forcing Dockerfile semantics into generic JSON rules; it must
  emit through the same validated door a pack does, but may parse however it
  likes. (This retires the first draft's "core recognizers are built-in
  packs" claim.)
- **Back-compat**: routes/manifests/languages `add/list/remove` are shipped
  surface and keep working — they become aliases into the same pack registry;
  no new lane ever grows its own verb family.

### Binding rule 2 — scalability & performance gates are EXECUTABLE, not prose (REVISED after external review, 2026-07-16)

The first draft said "stays ≲1s" — directionally right, unenforceable as
written ("O(bytes) is necessary but not sufficient: five linear scans are
still five scans, and allocation-heavy processing stays linear while getting
slower"). Revised: a `task bench-extract` harness is the gate, and it runs
CUMULATIVELY (all default recognizers on), not per-move.

Each benchmark record (committed to `proof/bench/`): corpus name + immutable
commit, file count + total bytes, machine + Go version, cold gather p50/p95
(≥10 runs), 5-file incremental refresh p50/p95, peak RSS, store bytes/nodes/
edges, query/card p50/p95 (later: trace), correctness fixtures, baseline
commit vs candidate commit.

Merge gates:

| Gate | Threshold |
|---|---|
| this repo, full gather | p95 ≤ 1s |
| large-corpus gather throughput regression | ≤ 5% |
| 5-file refresh regression | ≤ 5% |
| query p95 regression | ≤ 10% |
| peak RSS regression | ≤ 10% |
| store growth | proportional to useful emitted facts |
| duplicate semantic edges | zero |
| gate scope | cumulative, all default recognizers enabled |

Design constraints stay: recognizers are single-pass inside the existing
per-module fan-out; post-passes graph-linear; explicit size caps; the
dashboard's budgeted-payload rule extends to every new node kind.

### Move 1 — dev-env lane: "how do I run this locally" (manifests producer)

New core recognizers, all in the existing manifests lane:

- **Taskfile.yml** (+ `Taskfile.{dev,prod,qa,local}.yml`) → `task` nodes with
  the command line and desc (yamlwalk — it is exactly the YAML shape yamlwalk
  already flattens for k8s).
- **Makefile / justfile** → `task` nodes (line-anchored `^target:` scan — same
  literal-or-silent contract as the config lane).
- **compose.yaml / docker-compose.yml** → `service` nodes with image, ports,
  env keys, `depends_on` edges service→service, `uses_image` edges into the
  existing `image` kind (yamlwalk).
- **Dockerfile** → `image` node with base (`FROM`), exposed ports, entrypoint
  (line-anchored scan).
- **.env.example / .env.sample** → `config` key nodes (NAMES only — values are
  never stored, matching the secrets rule). Review tightening: multiple env
  layers (compose `environment:`, .env.*, devcontainer) need a stated
  precedence rule before edges claim "X configures service Y"; v1 emits keys
  + `declared_in` provenance only, no cross-layer resolution.
- **.devcontainer/devcontainer.json** → `config` node with the image/features.

Pack door: `packs.go` `validEmits` currently allows only `dependency|task`;
widen to `service|config` so the tail (Procfile, docker bake, tilt, skaffold)
stays pack-able without touching core.

### Move 2 — CI/CD lane: "what runs on a PR" (new `ci` recognizer family)

- **.github/workflows/*.yml** → `workflow` node (triggers from `on:`), `job`
  nodes, step commands; `runs` edges workflow→job→task. When a step invokes a
  task the dev-env lane already knows, the edge lands on that SAME task node —
  "does CI run what I run locally?" becomes a path query.
- **Linking needs command normalization + confidence semantics** (review
  tightening): `task ci`, `task --silent ci`, shell wrappers, and matrix
  substitutions are not identical strings. Exact command match → EXTRACTED;
  normalized match (strip flags/wrappers) → INFERRED; matrix/expression
  contexts (`${{ }}`) → skip rather than guess (literal-or-silent, the
  yamlwalk contract). Reusable/composite workflows are out of scope for v1.
- GitLab CI / Jenkinsfile / CircleCI: packs via the fact-pack contract, not
  core.

### Move 3 — `tests-for`: a DERIVED VIEW, not a persisted edge (REVISED after external review, 2026-07-16)

The first draft proposed persisting `tested_by` edges. External review
falsified the premise, and verification against the live store confirmed it:
`affected EnsureGlobalPointer` ALREADY returns
`TestGlobalPointerCreatesMissingFile` and `TestGlobalPointerLifecycle` at d1
via existing incoming `calls` edges — `Affected` walks edges backward
(internal/analyze/analyze.go). A persisted `tested_by` edge would duplicate
reachability, grow the store, complicate relation semantics, risk duplicate
impact rows, and (as a source→test outgoing edge) point the wrong way for
`affected`'s incoming walk.

Revised: **`ctx-optimize tests-for <symbol>`** — request-time computation,
zero store growth:

- Filter `affected`'s incoming callers to recognized test declarations
  (naming/path conventions: `_test.go`, `test_*.py`, `*.spec.ts`,
  `*.Tests/`…), transitively to a bounded depth.
- Answer with confidence semantics: direct EXTRACTED call vs INFERRED
  resolution vs co-change-only evidence, plus unresolved-dispatch counts.
- The same request-time classifier feeds `change-plan` and `review-diff`'s
  "relevant tests" row (companion ADR) — which therefore no longer waits on
  any new edge.

A persisted relation returns ONLY if a benchmark proves the derived view is
too slow or misses cases a materialized edge would catch.

### Move 4 — schema lane: "what is the actual DB schema"

- **FK edges in core SQL**: parse `REFERENCES` in `create_table` → `references`
  edges table→table (graphify has this; we parse the same tree-sitter grammar
  and simply drop the relation today).
- **Migrations ordering**: NOT name-sorted-by-universal-convention (first
  draft overclaimed — Django uses dependency declarations, Rails timestamps,
  Flyway version tables, some systems code-defined order). Each framework's
  ordering needs its own accuracy spike before `migration` nodes with
  sequence + `applies_to` edges ship; start with the one the first real user
  repo actually uses.
- **ORM schemas as packs**: prisma (grammar exists upstream — addable by
  name), Django/JPA/GORM models (route-style declaration packs). Tail, not
  core.
- **Live DB introspection stays OUT of the binary** (no DB drivers — ever).
  The door already exists: a skill-level **adapter asset** that shells
  `psql`/`pg_dump --schema-only` and emits batch JSON through `add --json`.
  Ship it as a documented adapter example, like graphify's opt-in — but with
  the dependency in the adapter script, not the product.

### Move 5 — question-shaped wiki pages: where "no-LLM answers" becomes visible

The wiki already renders from nodes+edges only. Add four pages, each a pure
render of the lanes above, each line carrying file:line citations:

- **RUNBOOK.md** — tasks (local + CI), services, ports, env keys, images.
- **PIPELINE.md** — workflows, triggers, jobs, and which local tasks CI runs.
- **TESTING.md** — test tasks, test counts per module, top `tested_by` hubs.
- **SCHEMA.md** — tables, FK graph, migration order.

This is the payoff shape: the agent opens ONE page (or one query) and answers
"how do I run this" with citations; the LLM contributes phrasing, nothing
else. It is also the demo: `ctx-optimize wiki` on a fresh clone producing a
correct RUNBOOK is self-evidently "value without an LLM".

### Cheap adjacent wins — all DEFERRED after review

- **Rationale nodes** (`// NOTE|WHY|HACK` → nodes): deferred — expands index
  and query noise without connecting to a proven user question yet.
- **Grow the addable-by-name grammar list**: dropped — no demonstrated
  demand (composed-answers ADR).
- Pack-emit widening is no longer a "cheap win" — it is step 1 of the plan
  (the fact-pack contract), designed properly.

## What we will NOT do (the core, restated)

- No LLM calls, embeddings, or vector stores in the binary — media semantics
  (PDF/image/video) stay out; that is graphify's lane and it requires paid
  inference, which contradicts "gather on every commit for free".
- No DB drivers / no network beyond the configured remote — live introspection
  only via adapter scripts.
- No YAML library (stdlib rule) — yamlwalk's literal-or-silent contract covers
  every shape above; anything it can't represent is dropped, not guessed.

## Sequencing & measurement

SUPERSEDED on sequencing by `2026-07-16-composed-answers/proposal.md` (the
reviewed external proposal): the merged, dependency-honest order interleaves
these lanes with answer-side composition — confidence block → trace/
change-plan → Move 3 (tested_by) → review-diff → Moves 1–2 → Move 4 → Move 5.
The moves themselves and both binding rules are unchanged; the "grow the
addable-by-name grammar list" cheap-win is dropped (no demonstrated demand).
Each move still ships alone behind `task ci`, spec-first per house rules.

Measure like the proof matrix: for each question class, one A/B on this repo +
one real polyglot repo — store-answer tokens + correctness vs grep-and-read
baseline, recorded in `proof/`. No staged numbers; session-level claims only
with the "varies by question/repo" caveat (per the 2026-07-15 honesty rule).
