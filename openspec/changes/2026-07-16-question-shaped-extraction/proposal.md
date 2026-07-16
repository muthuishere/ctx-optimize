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

### Binding rule 1 — every lane ships its pack door IN THE SAME CHANGE (maintainer, 2026-07-16)

Extensibility is not a follow-up. A new lane that lands core-only is
REJECTED at review: the change that adds a lane must also add the drop-in
door for additional parsers, exactly as routes/manifests/languages have
today. Concretely:

- The pack schema (`internal/extract/manifests/packs.go`) widens its
  `validEmits` from `{dependency, task}` to include every kind a new lane
  introduces — `service`, `config`, `workflow`, `job`, `migration`, `test` —
  and pack rules gain edge emission (`depends_on`, `runs`, `references`,
  `tested_by`) with the same fail-closed validation as nodes.
- Each move's core recognizers are themselves expressed through the SAME rule
  shape the packs use (the routes-lane precedent: core recognizers are
  built-in packs). GitHub Actions is the first CI pack, compose the first
  service pack — GitLab CI, Jenkins, Procfile, tilt, skaffold, ORM schemas
  are then user-addable JSON, no binary release needed.
- `ctx-optimize <lane> add <name|github-url>` / `list` / `remove` work on
  day one for every new lane — the CLI surface is part of the lane, not
  polish.

### Binding rule 2 — scalability & performance are first-class, NOT optional (maintainer, 2026-07-16)

The product promise is "gather in about a second, refresh on every commit".
Every move below is admitted only with a measured performance budget, and a
change that regresses the gather gate does not merge:

- **Gather budget**: the 4k-file reference repo stays ≲1s end-to-end after
  ALL five moves. Each new recognizer is O(bytes) single-pass (yamlwalk /
  line-anchored scans — no YAML/AST library, no backtracking); recognizers
  run inside the existing per-module parallel fan-out, never as extra passes
  over the tree.
- **Post-passes are graph-linear**: `tested_by` is O(E) over already-loaded
  edges; wiki pages render O(nodes+edges) like the existing pages. Nothing
  quadratic, nothing that loads the graph twice.
- **Scale ceilings are explicit**: recognizers skip files over the existing
  size caps; pack rule counts are bounded (fail loudly, never slow-crawl);
  the dashboard's budgeted-payload rule extends to every new node kind
  (a 50k-task monorepo must not blow up /api/graph — same top-N + sample
  fairness as producers get today).
- **Every move's tasks.md carries a perf line**: the measured before/after
  gather time on this repo AND one large corpus (chromium subset already in
  the store), recorded in `proof/` next to the correctness A/B. A move
  without its perf measurement is not done.

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
  never stored, matching the secrets rule).
- **.devcontainer/devcontainer.json** → `config` node with the image/features.

Pack door: `packs.go` `validEmits` currently allows only `dependency|task`;
widen to `service|config` so the tail (Procfile, docker bake, tilt, skaffold)
stays pack-able without touching core.

### Move 2 — CI/CD lane: "what runs on a PR" (new `ci` recognizer family)

- **.github/workflows/*.yml** → `workflow` node (triggers from `on:`), `job`
  nodes, step commands; `runs` edges workflow→job→task. When a step invokes a
  task the dev-env lane already knows (`task ci`, `npm test`, `go test`), the
  edge lands on that SAME task node — CI and local dev join into one graph,
  which is the whole point: "does CI run what I run locally?" becomes a path
  query.
- GitLab CI / Jenkinsfile / CircleCI: packs, not core (same doctrine as
  routes).

### Move 3 — test topology: `tested_by` as a pure post-pass (zero new parsing)

Verified 2026-07-16: test→source `calls` edges already exist in the store
(`project_test.go::TestGlobalPointerLifecycle → project.go::EnsureGlobalPointer`).
The move is a producer post-pass, not an extractor:

- Classify decl nodes in test files (existing conventions: `_test.go`,
  `test_*.py`, `*.spec.ts`, `*.Tests` csproj…) as `kind: test`.
- Reverse their `calls` edges into `tested_by` edges (INFERRED confidence,
  like call resolution).
- `affected <symbol>` then answers the question agents ask before every edit:
  **"which tests do I run for this change?"** — today that is a grep.

This was Move 3 of the 2026-07-14 modules ADR, deferred; the multi-path module
work (test→source calls resolving across scattered folders) made it cheap.

### Move 4 — schema lane: "what is the actual DB schema"

- **FK edges in core SQL**: parse `REFERENCES` in `create_table` → `references`
  edges table→table (graphify has this; we parse the same tree-sitter grammar
  and simply drop the relation today).
- **Migrations ordering**: a migrations dir (name-sorted, the universal
  convention) → `migration` nodes with sequence edges + `applies_to` edges
  into table nodes. The "current schema" is then a walk, not a guess.
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

### Cheap adjacent wins (from the graphify comparison, core-safe)

- **Rationale nodes**: `// NOTE|WHY|HACK|SAFETY` comments → `rationale` nodes
  attached to the enclosing decl (we already walk every file; zero new IO).
- **Grow the addable-by-name grammar list** toward their ~40 (each is a
  build-once pack; no binary growth).
- Widen `validEmits` (Move 1) — one-line unlock for the pack ecosystem.

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
