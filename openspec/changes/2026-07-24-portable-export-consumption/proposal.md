# ADR — native, fast, cross-module filtering: the CLI answers, not `jq`

Status: APPROVED v3 — 2026-07-24. Spike numbers in `spikes.md` (native ~4×
faster, 47–64× less memory than jq/python, and the only portable path). UX
chosen: **UX-2 — typed `nodes`/`edges` verbs + `deps` alias**, filter flags on
`export` sharing the same core, `export --jq` (gojq) escape hatch. Sub-
decisions locked in "Decisions" below. Implementation may proceed with the
golden-gate additions.

## Implementation status (2026-07-24)

SHIPPED on `feat/native-filters` (task ci + task golden green):
- `internal/graphfilter` — shared predicate + projection core + tests.
- `nodes` / `edges` / `deps` verbs (table default, `--json`/`--ndjson`,
  `--select`, federated at root). `deps` surfaces `scopes` top-level and
  `--importers` does the two-hop join in one command (kills the #5 jq monster).
- `export` filter flags + `--ndjson` (bare export byte-identical — non-breaking).
- `query` pre-rank narrowing at all three call sites.
- Golden: hermetic verb tests (deterministic output, importer join, query
  narrowing) + a filter-perf wall guard.
- SKILL.md routing rows: explicit "use `nodes`/`edges`/`deps`, NEVER `export | jq`".
- CLI `--help` for the new verbs.

DEFERRED (follow-ups, documented): gojq `--jq` escape hatch; `--select`/
`--ndjson` projection on card/change-plan/path/explain/verify + `affected`
`--kind` post-filter; the remaining 6 agent surfaces (instructions template +
version stamp, hook-context strings, pointer blocks, README/cookbook); the
streaming ReadBytes O(1) fast path (v1 reuses the store's already-safe 16 MB
loader, so the jq-ratio/RSS floor from the stress test applies to that future
lane, not v1 — v1 has a wall-ceiling guard instead).

## Decisions (locked)

- **Surface**: `nodes` / `edges` verbs (table default; `--json`/`--ndjson`
  opt-in), federated across all modules at root via `loadGraphScoped`; `deps`
  = `nodes --kind dependency` alias (with `--importers`, `--scope`). Same
  filter core reused by `export`'s new filter flags. `export --jq` = gojq
  escape hatch (NOT the fast path).
- **Universal read-surface rule** (owner 2026-07-24 — "all search commands"):
  NO read/search verb should ever need jq. The shared `internal/graphfilter`
  predicate plugs into EVERY read verb, at each verb's NATURAL point:
  - **`query` — PRE-rank narrowing** (semantics pinned): apply
    `graphfilter.Apply(nodes, edges, pred)` to the slices BEFORE
    `query.Run` (verified drop-in: query.go:81 takes the slices; all call
    sites — app.go:952 single, ~979 module, `federatedQuery`→`loadFederated`
    ~1027 — materialize them first). This ranks WITHIN the filter, so the
    budget/top-N is spent on matching records ("top react **files**" can't be
    crowded out by a higher-scoring decl). Federation-free: Apply runs per
    module inside the concat federatedQuery already does. Flags:
    `query "<q>" [--kind K] [--file-type FT] [--relation R] [--where k=v]
    [--scope S] [--select …] [--ndjson]`; bare `query "<q>"` stays
    byte-identical.
  - **`hubs`** — same pre-rank narrowing (rank hubs WITHIN the kind).
  - **`affected`** (blast-set result) — POST-filter the produced set
    (`--kind test` → the impacted tests) + `--select`/`--ndjson`.
  - **`card`, `change-plan`, `path`, `explain`, `verify`** (single-answer) —
    `--select`/`--ndjson` projection so JSON is directly consumable (pluck
    `location`/`signature` without jq).
  - `nodes`/`edges`/`deps`/`export` as already specified.
  ONE predicate struct, FIVE+ consumers (nodes, edges, export, query, hubs,
  affected, …); no per-verb reimplementation, no mini-language.
- **Fast path**: stream the store ndjson, decode per line, emit matches —
  O(1) memory (spike). Never load-then-filter.
- **gojq placement**: MAIN binary — `export` is a main read verb; gojq is pure
  Go, modest size, no cgo. (Revisit only if it moves the query-noise budget.)
- **Aliases now**: `deps` only. `routes`/`k8s` stay `nodes --kind …` until
  asked twice.
- **Speed floor**: CI pins native filter time on a fixed corpus AND commits to
  **≥3× the jq baseline** (headroom under the measured ~4×); may only move up.
Trigger: issue #5 follow-up — CONSUMERS (and agents) pipe `export`/`query
--json` into `jq`, which is absent on Windows / minimal CI, so they fall back
to slow throwaway `python`. (Audit finding 2026-07-24: our own shipped docs
and skills contain NO such pipes — they're clean; the pain is external
consumers + agent habit, and the fix is to ADD a native surface, not rewrite
bad pipes.) Owner's framing: **the tool must answer these questions natively
and *extremely fast*, across every node/edge kind, every module, and every
read verb — jq or a hand-rolled script will always be slower and less
portable.**

## Context — export is all-or-nothing, single-module, and pushes work out

`cmdExport` (internal/app/app.go L1764) emits the WHOLE graph as one
`{"nodes":[…],"edges":[…]}` blob (L1796) from ONE module store (`openStore`,
L191). Two consequences:

1. **Every real question is external post-processing.** "Just the k8s
   services", "resolves_to edges", "dev-scope deps + who imports them" all
   force `| jq` or `| python`.
2. **It's the slow path by construction.** That pipe (a) serializes the
   entire graph to JSON, (b) spawns a second process, (c) re-parses the whole
   document into a second value model, (d) serializes matches. The graph was
   already in memory as Go structs — we throw that away and pay for a full
   round-trip plus a subprocess. On a 160k-node store that is seconds and a
   dependency the user may not have.

Native, in-process filtering is a single O(n) pass over structs we already
hold, emitting only matches — no full serialization, no subprocess, no second
parse. **That speed gap IS the feature**; portability comes free with it.

**Real proof this is the actual habit (issue #5, 2026-07-24):** to get
per-file external usage with scope, a real consumer wrote a **20-line
`export --format json | jq` three-way join** (imports ⋈ resolves_to ⋈ dep
nodes). That is the pain, verbatim, from a paying use case. Our native surface
must collapse it to ONE portable command — this becomes success-check #4 and a
scoreboard case below:

```sh
# their jq monster ⇒ one native command:
ctx-optimize deps --importers --select importer,id,scope   # file → dep → scope, all modules, no jq
```

## Requirements (owner, widened)

- **Native filter flags**, fast, first-class — not a grudging escape hatch.
- **Across every kind**: dependency, k8s (deployment/service/configmap/
  ingress/secret/image), route, task, source, file, decl, module — anything
  the graph carries. Filtering is generic over `kind` / `relation` /
  `metadata`, so new producers are filterable the day they land, no new flag.
- **Across every module**: at a monorepo root it filters the FEDERATED graph
  (all modules + root residual), like `query` — reuse `loadGraphScoped`
  (app.go L1257), which already federates at root scope and honors `--root`.
- **Extremely fast**, and the speed is *pinned in CI* (golden floor), not just
  claimed.
- **Great UX**: a human gets a readable answer with no flags-soup; machines
  get `--json`/`--ndjson`. Portable everywhere (one static binary).
- **The skills teach the native path** so agents stop reaching for jq.
- **gojq escape hatch** for arbitrary shaping, so existing jq muscle memory
  still works cross-platform without external jq.

## UX options to settle (the open discussion)

### UX-1 — filter flags on `export`
`export --kind service --where namespace=prod --ndjson`,
`export --relation resolves_to`. One verb, every format inherits the filter.
- + one place; pipeline-friendly; `--ndjson` makes it grep/findstr-able.
- − `export` reads as "dump for other tools"; a human wants a readable table,
  and flag-stacking is not the friendliest first-run.

### UX-2 — two typed verbs mirroring the record types
`ctx-optimize nodes [--kind …] [--where …] [--select …]` and
`ctx-optimize edges [--relation …] [--from X] [--to Y] [--where …]`.
Table by default (human), `--json`/`--ndjson` for machines. Federates at root.
- + mirrors the data model exactly; discoverable via `--help`; readable
  default; covers "network/k8s/all kinds" generically (`nodes --kind service`,
  `edges --relation routes_to`).
- + thin semantic aliases for the top asks: `deps` = `nodes --kind
  dependency`; optionally `routes`, `k8s`.
- − two new verbs to document; slight overlap with `query` (but `query` is
  ranked lexical search; these are exact structured filters — different job).

### UX-3 — one `ls`/`find` verb with a target word
`ctx-optimize ls deps --scope dev`, `ctx-optimize ls k8s`, `ctx-optimize ls
edges --relation resolves_to`.
- + one memorable verb; friendly; presets for common questions.
- − a target-word grammar to invent and maintain; less mechanical than
  nodes/edges.

**Recommendation for discussion: UX-2** (typed `nodes`/`edges` verbs, table
default, semantic aliases) as the primary human+machine surface, **filter
flags on `export` (UX-1) reusing the SAME filter core** for the pipeline/all-
formats path, and **`export --jq` (gojq)** as the arbitrary-shape hatch.
All three share one `internal/graphfilter` predicate engine — no duplicate
logic. Rejecting a bespoke `--where` mini-language beyond simple `k=v` / `k~v`
(contains): that's what `--jq` is for.

## Compatibility — NON-breaking (minor bump, → 0.8.0)

Everything is additive:
- New `nodes`/`edges`/`deps` verbs — no prior behavior.
- New `export` filter flags — `export --format json` with no filters emits the
  identical whole-graph blob as today.
- gojq is an internal dep; ctx-optimize is a CLI, not a consumed Go API.

**The one break to AVOID**: do NOT switch `export`'s default scope from
single-module (`openStore`) to federated (`loadGraphScoped`) — at a monorepo
root that silently changes `export` output (root-residual → all-module union).
Decision: `export` default stays single-module; **federation-by-default lives
in the new verbs** (no prior behavior to break); `export` gets `--root` /
`--modules` as OPT-IN. Result: zero existing invocation changes behavior.

## Filter core (shared, generic, fast)

**Stream the ndjson, don't load-then-filter** (spike + stress-test): the store
already persists nodes/edges as ndjson, so the filter decodes record-by-record
and emits matches with O(1) memory — measured RSS dead-flat at ~12 MB from
220k to 4.4M edges, ~2× faster than jq-streaming (which stays flat too) and
the ONLY path that doesn't grow RSS linearly (jq -s and python `json.load`
hit 5–6 GB at 4.4M edges → OOM on a 16 GB laptop ~40×). Federated root reads
concatenate each module's stream in turn.

**MUST-HANDLE (stress-test hard requirements — a plausible-but-wrong count is
worse than a loud failure):**
1. **Unbounded line reader** — use `bufio.Reader.ReadBytes('\n')`, NOT a
   fixed-cap `bufio.Scanner`. A single over-cap line makes Scanner abort and
   **silently truncate the rest of the stream** (returned 1 of 3 records in
   test). ctx-optimize lines legitimately carry large signature/metadata
   blobs. If Scanner is ever kept, cap ≥ 16 MB AND treat `sc.Err()` as a hard
   error, never a partial success.
2. **Skip-and-continue on malformed lines** (truncated JSON, non-JSON, wrong-
   shape array/scalar); count them, optionally surface `malformed=N`. Never
   abort the run.
3. Skip blank/whitespace-only lines; process a final line with no trailing
   newline.
4. Missing/`null` metadata under a `--where` = no-match, not crash. Same for
   missing kind/relation/id.
5. **Decode metadata lazily** — only when a `--where` is present (the flat
   12 MB / 187 MB/s depends on not paying for the map otherwise).
6. No dedup expectation — duplicate ids stream through independently.

One predicate pass over the (streamed, federated) graph:
- node: `kind ∈ set`, `file_type ∈ set`, `id`-prefix, `label` contains,
  `producer ==`, and `where` conds (`k=v` exact / `k~v` contains) resolved
  against top-level fields OR `metadata.<k>`.
- edge: `relation ∈ set`, `confidence ∈ set`, `from`/`to` id match,
  `producer`, `where`.
- projection `--select a,b,metadata.scopes`; streaming `--ndjson`; table
  default for verbs.
Comma-separates OR within a dimension; multiple flags AND across dimensions.
`--where` takes comma-separated ANDed conds (no parser change needed).

## Spike results (DONE 2026-07-24 — see spikes.md + stress-test)

Measured on federated mastra (220k edges), scaled to 4.4M:
- **Native streaming: linear wall (~1.05M lines/s), RSS dead-flat ~12 MB.**
  ~2× faster than jq-streaming at every size; jq-slurp / python `json.load`
  grow to 5–6 GB at 4.4M (OOM path). Native is the only stock-Windows/Alpine
  option. Correctness cross-checked EXACT vs jq on 6 predicates.
- Sharpest trap found: fixed-cap `bufio.Scanner` silently truncates on a long
  line → MUST-HANDLE #1 above.

## Agent-surface update inventory (audit 2026-07-24 — ALL wait-for-code)

The skills teach `query`/`card`/`change-plan`/`affected`/`path` and `export`
only as tool-interchange; none teach a list/filter path. Add the native verbs
to each of these, ONLY after the verbs ship (referencing non-existent commands
breaks skill-vs-reality):

1. **SAME PR as code** — CLI help must match the binary: `cmdExport` doc
   (app.go:1761) + `export` usage (app.go:2667) + `nodes`/`edges`/`deps` in
   main usage help.
2. Embedded skill routing table `internal/skills/bundled/ctx-optimize/
   SKILL.md:183` (highest agent leverage) — add nodes/edges/deps rows.
3. `references/activation-routing.xml:235-240` — new FILTER/LIST routes.
4. Committed template `internal/project/templates/instructions.md:26-36`
   verb table — add a List/filter row; **bump the managed-block version
   stamp** so init/up re-emit into user repos.
5. Hook-context strings `app.go:1527,1544,1549` (keep all three in sync).
6. Pointer-block generators `project.go:326-361` + `globalBlock` (505-520);
   then regenerate this repo's own CLAUDE.md/AGENTS.md blocks.
7. Docs: README.md/npm/README.md L36 + L152, docs/cookbook.md:64, optional
   push-pull.md:93 cross-ref, optional small-model protocol
   (instructions.md:122-139).

(Adapter-ingestion `.py | ctx-optimize add` mentions and the benchmark's
graphify install are OUT of scope — data IN, not consumption OUT.)

## Tightened golden gate (stress-test-derived numbers, per owner)

- **Perf floor — two metrics on the pinned mastra `big-edges.ndjson` (220,566
  edges), predicate `relation=imports`:**
  - **Ratio (machine-independent, primary):** native wall ≤ **0.70 ×**
    jq-streaming wall on the same box (measured 0.50; cancels CI-runner speed,
    still catches a 40% regression).
  - **RSS ceiling (the streaming-contract guardrail):** native peak ≤ **40 MB**
    (measured 12 MB). This is the number that fails loudly if anyone
    reintroduces a load-all path — wall might stay fine, RSS jumps 20–500×.
  - Absolute wall ceiling: ≤ **0.60 s** on 1× corpus (measured 0.21, ~3×
    headroom for cold CI). All "may only move up."
- **Deterministic snapshots** (stress-test-recommended, exact ints/strings):
  - match counts on mastra: `kind=function`→13774, `relation=imports`→38468,
    `confidence=INFERRED`→42819, `--where producer=code`→62437,
    `kind=method`+`--where lang=typescript`→15119.
  - **long-line fixture MUST return 3 records** (the single most valuable pin —
    a regression to a fixed-cap Scanner returns 1 and fails this).
  - malformed fixture → `total=7 matched=3 malformed=4 empty=2`;
    missing/null → `kind=function`=4, `--where producer=code`=5;
    match-all → full node count, `--where` nonexistent key → 0.
- **Scoreboard questions**: add k8s / route / dependency-scope AND a
  **`query`+filter** question (e.g. "the file that renders X" via
  `query --kind file`) so the composed pre-rank-narrowing answer is
  quality-pinned, not just present.
- Extend the corpus tier to exercise a k8s + multi-module fixture end to end.

## Open questions — ALL RESOLVED 2026-07-24

1. UX shape → **UX-2** (typed verbs + `deps`). 2. Default output → **table**,
`--json`/`--ndjson` opt-in. 3. gojq → **main binary**. 4. Aliases → **`deps`
now**, routes/k8s later. 5. Speed floor → **ratio ≤0.70× jq + RSS ≤40 MB**
(stress-test). Nothing left blocking implementation.

## Success check

- On stock Windows/Alpine with nothing else installed, a user answers "list
  prod k8s services", "which files use react", and "dev-scope deps" each with
  ONE ctx-optimize command, across all modules, in well under a second on a
  big store.
- **#4 — the issue-#5 jq monster dies**: the 20-line `export | jq` three-way
  join (file → dep → scope) is replaced by one `deps --importers` command,
  same output, no external tool. Pinned as a scoreboard case.
- README/issue examples for consuming the graph use no external binary.
- Golden gate carries a speed floor and deterministic snapshots for the new
  surface; scoreboard covers the new question shapes.
