# Cross-tool benchmark — run notes (2026-07-24)

Honest-benchmark run per `openspec/changes/2026-07-24-cross-tool-benchmark/proposal.md`.
Everything below was produced by commands actually executed in this arena
(`~/ctx-bench-arena/`) on this machine. No number in `results-multi.json` or here
is estimated.

## Machine

`Apple M5 Pro, 18 cores, 48 GB` — verified via `sysctl -n machdep.cpu.brand_string
hw.ncpu hw.memsize` (51539607552 bytes = 48 GiB, rounded).

## Tool versions + cloned commits

| tool | version | source | commit SHA |
|---|---|---|---|
| ctx-optimize | v0.7.0-dirty (66c9705, 2026-07-24T04:51:00Z) | this repo, bin/ctx-optimize copied into arena | 66c9705 (repo HEAD at run time) |
| graphify | 0.9.12 | `git clone --depth 1 https://github.com/safishamsi/graphify.git` | 2fa6cd3d5548577f8c5f591b713f0bf80c1af183 |
| CodeGraph | 1.5.0 (`@colbymchenry/codegraph`) | `git clone --depth 1 https://github.com/colbymchenry/codegraph.git`, local `npm install && npm run build`, invoked via local `dist/bin/codegraph.js` | 572d22bfbe82602080e457bec655f72e3314f9ef |
| GitNexus | 1.6.9 | `git clone --depth 1 https://github.com/abhigyanpatwari/GitNexus.git`; monorepo — installed root `npm install`, built `gitnexus-shared` then `gitnexus` package (`npm run build` in each), invoked via local `gitnexus/dist/cli/index.js` | 91b22676ceaa66ce7941fcb146ffc68ff9a144e6 |
| ast-grep | 0.45.0 | **pre-installed globally**, per instructions — NOT cloned/pinned like the others | n/a |

`graphify` PyPI package name is `graphifyy` (author Safi Shamsi); the CLI binary
is still called `graphify`. Confirmed the upstream repo via the installed
package's `Project-URL: Repository` metadata since a first guess
(`muthuishere/graphify`) 404'd.

## Corpora

| corpus | files | source |
|---|---|---|
| corpus-flask | 265 | copied verbatim from `benchmarks/corpus-flask` (pre-existing, pinned Flask clone) |
| corpus-gin | 159 | copied verbatim from `benchmarks/corpus-gin` (pre-existing, pinned Gin clone) |
| corpus-ctx-src | 207 | this repo's `internal/` + `cmd/` only, no `.git`/vendor/store |
| corpus-graphify-src | 754 | the graphify clone's full tree minus `.git` (code + docs + tests + fixtures) |

**Caveat (spec correction):** the proposal describes `corpus-graphify-src` as
"~12k files" and calls it "the large corpus." The real graphify repo has 754
files total (260 `.py`). It is NOT a 12k-file corpus — that number in the ADR
was an unverified assumption, not a measurement, and I did not manufacture a
fake 12k-file tree to match it. `corpus-graphify-src` is still real and still
the largest of the four (754 vs 265/207/159), so the four-corpus spread is
intact, just smaller in absolute terms than the ADR assumed. This is the most
material fairness note in this run — flag it before publishing any "12k
files" claim on the site.

Every gather tool got a **fresh copy** of each corpus (`.codegraph/`,
`.gitnexus/`, `graphify-out/` cleaned before each cold run) so no tool's store
leaked into another's timing or `store_bytes`.

## What ran (bench_multi.py — see also `benchmarks/bench_multi.py` in the repo
if committed; this run used a scratch copy with identical logic)

Axis 1+2 (cold gather, best of 3 + 1 warm run, node/edge counts, store bytes)
and axis 3 (query latency, median of 5, budget 2000) for:
**ctx-optimize, graphify, CodeGraph, GitNexus, ast-grep** — all four corpora.

- ctx-optimize: `add <path>` (cold), `add <path>` again (warm), `status --path <path> --json`.
- graphify: `update <path> --no-cluster` (cold+warm) — no clustering/LLM, its fastest path.
- CodeGraph: `init <path>` (cold, store wiped first each of the 3 runs), `sync <path>` (warm — `init` refuses on an already-initialized dir, `sync` is the tool's own designated incremental-update path), `status <path> -j` for counts. `CODEGRAPH_TELEMETRY=0` set to avoid a network call in the timed path.
- GitNexus: `analyze <path> --skip-git --index-only --name <corpus>` (cold, store wiped first each of 3 runs; warm = same command again, GitNexus detects the unchanged tree and does an incremental pass). No `--embeddings`/`--skills`/`--pdg` — deterministic, no model calls. Node/edge counts parsed from the tool's own stdout summary line (`N nodes | N edges | ...`); it has no `status --json` counts. Log noted `FTS extension unavailable; continuing without FTS features` in this sandbox — did not attempt to install it (out of scope, would change the timed path).
- ast-grep: `ast-grep run -p '<pattern>' -l <lang> <path>` — no gather row (no index), query-axis only, one "find all functions" structural pattern per corpus/language.

Full per-command logs (cmd, rc, stdout, stderr) are under `~/ctx-bench-arena/logs/`,
one file per (tool, corpus, phase).

Query questions (fixed per corpus, same text handed to every tool's CLI verbatim
except codegraph, which takes a symbol term not free text — see below):

| corpus | question |
|---|---|
| corpus-flask | "route decorator handling" |
| corpus-gin | "router group handling" |
| corpus-ctx-src | "store merge producer" |
| corpus-graphify-src | "graph extraction parse" |

CodeGraph's `query` command is a symbol-name search, not a free-text question
(`query <search>` — filters by symbol name/kind), so it got the first word of
the question (`route`, `router`, `store`, `graph`) — recorded as
`codegraph_query_term` in `results-multi.json`. This is arguably not the
"same" query as the other tools' free-text search; it is CodeGraph's own
closest CLI verb doing the analogous job. Flagged for the auditor.

ast-grep pattern/lang paired with each corpus for the query axis (structural
"find all functions" scan, no index — this is the "scan every time" baseline,
not a claim ast-grep is worse):

| corpus | pattern | lang |
|---|---|---|
| corpus-flask | `def $NAME($$$):` | python |
| corpus-gin | `func $NAME($$$) { $$$ }` | go |
| corpus-ctx-src | `func $NAME($$$) { $$$ }` | go |
| corpus-graphify-src | `def $NAME($$$):` | python |

## Results summary (see `results-multi.json` for full numbers)

All 4 gather tools succeeded (no `error` field set) on all 4 corpora, and all
5 query tools produced a real median-of-5 timing on all 4 corpora. No cell
was skipped or faked.

Rough shape (full precision in the JSON): ctx-optimize's cold gather was the
fastest on every corpus (0.5s–1.2s); CodeGraph was close behind on smaller
corpora and fastest on `corpus-graphify-src` cold (1.9s) with by far the
fastest warm/sync (0.13–0.2s everywhere); graphify's `--no-cluster` path
landed in the 0.8s–8.6s range; GitNexus was consistently the slowest gather
(6.5s–27.9s) but is also doing materially more work by default (community
detection / flow clustering / FTS-index build attempt) that the others don't
do in their timed path — noted as an honesty caveat, not papered over.

Query latency: ctx-optimize and ast-grep were both sub-100ms across the
board; graphify and CodeGraph were in the low hundreds of ms; GitNexus's CLI
query was consistently the slowest (1.9s–3.8s) — its own log shows most of
that wall time in a BM25 text-search stage, not symbol lookup.

## Serena / potpie — omitted, per the ADR's pre-verified feasibility table

Per `openspec/changes/2026-07-24-cross-tool-benchmark/proposal.md` (feasibility
verified 2026-07-24, before this run started):

- **Serena**: LSP-based (spawns a language server per language + a client
  session), no persistent on-disk graph artifact comparable to the other four
  — there is no "cold gather → store bytes" row that would be a fair,
  like-for-like comparison. Not cloned or spun up for this run; the ADR's
  own verdict ("different shape — timed separately, NOT in gather table") was
  taken at face value rather than re-derived, per the "don't sink time
  forcing them" instruction.
- **potpie**: requires a running Neo4j service. No single-machine, no-global-
  install row is fair without standing up a database service outside the
  arena's isolation contract. Not cloned or attempted.

Neither tool was run in this session. If a future run wants real Serena/potpie
rows, that is new work, not something this run silently skipped.

## Quality-capture axis (added mid-run, owner directive)

Deterministic, no-judging capture of each tool's raw query/context output for
14 (corpus, question) pairs against real symbols, saved verbatim to
`~/ctx-bench-arena/quality/<corpus>/<tool>__<qid>.txt` (plus a `grep` baseline
and byte sizes in `~/ctx-bench-arena/quality/context-sizes.json`). No answer
was judged or scored in this run — that's explicitly a separate step.

Per-tool verbs used:
- **ctx-optimize**: both `card <symbol>` and `query "<question>"`, saved
  separately (`ctx-optimize__card__qN.txt`, `ctx-optimize__query__qN.txt`).
- **graphify**: `query "<question>" --budget 2000 --graph <corpus>/graphify-out/graph.json`.
- **CodeGraph**: `explore <symbol>` (its one-shot "relevant source + call
  paths" verb) — no fallback needed, worked for all 14.
- **GitNexus**: `context <symbol>` (its "360-degree view" verb) — worked for
  13/14; one cell (corpus-graphify-src / q3 `cluster`) fell through to the
  `query "<question>"` fallback and succeeded there.
- **ast-grep**: a structural pattern matching the symbol's *definition* (not
  the looser "any function" pattern from the speed-axis query). Two patterns
  needed a fix mid-run (see below); all 14 now match.
- **baseline**: `grep -rn "<symbol>" <corpus>`, first 40 lines kept.

### Bug caught and fixed during capture

The first capture pass had two real problems, both fixed and the whole
capture re-run from scratch (not patched in place):

1. **ast-grep pattern bug (my error, not the tool's).** Four Python patterns
   included a trailing `:` (`def url_for($$$):`) which fails to match any
   function with a return-type annotation (`def url_for(...) -> str:`) —
   every real function in Flask/graphify has one, so all four Python `def`
   patterns and the `node_link_graph` call pattern matched **zero** results
   on the first pass (`rc=1`, empty). Fixed by dropping the trailing colon
   (`def url_for($$$)`) and, for `node_link_graph`, switching to
   `$X.node_link_graph($$$)` since the real call sites are all
   attribute calls (`json_graph.node_link_graph(...)`), not bare calls. All
   14 ast-grep captures are non-empty after the fix.
2. **Baseline grep swept the tools' own stores.** The first baseline pass ran
   plain `grep -rn <symbol> <corpus>` *after* the gather phase had already
   left `.codegraph/`, `.gitnexus/`, and `graphify-out/` inside each corpus
   dir — so the "no-tool" baseline was accidentally grepping through
   GitNexus's binary LadybugDB file and graphify's minified AST cache, one
   producing a 40-line file that was **7.7 MB** (one line = an entire
   generated JSON blob). Fixed with
   `--exclude-dir={.codegraph,.gitnexus,graphify-out}`; baseline files are
   now realistic sizes (a few KB, one legitimately larger at ~100 KB for
   `corpus-ctx-src`/`Merge` because that corpus includes a committed minified
   dashboard JS bundle that genuinely contains many "Merge"-adjacent matches
   — left as-is, it's real corpus content, not a store artifact).

### Coverage (98 capture cells = 14 qids × 7 rows: baseline + ctx-optimize×2 +
graphify + codegraph + gitnexus + ast-grep)

| tool | ok / total |
|---|---|
| baseline (grep) | 14/14 |
| ctx-optimize `query` | 14/14 |
| graphify `query` | 14/14 |
| codegraph `explore` | 14/14 |
| gitnexus `context` (+1 `query` fallback) | 14/14 |
| ast-grep (structural def pattern) | 14/14 |
| ctx-optimize `card` | **9/14** |

The 5 `ctx-optimize card` misses are **not failures** in the usual sense —
they're the tool's disambiguation guard refusing to silently guess when
multiple same-named symbols exist (e.g. `dispatch_request` is defined on
`Flask`, `MethodView`, `View`, and `AsyncView` in corpus-flask; `ServeHTTP` on
`Engine` and two test structs in corpus-gin). Each of those 5 `.txt` files
contains the real `rc=1` + the full candidate-disambiguation message from
stderr — that message is itself a legitimate, honest answer shape ("here are
5 things named X, be specific"), not an empty result. Full list: corpus-flask
q3 (`dispatch_request`), corpus-gin q2 (`ServeHTTP`) and q4 (`addRoute`),
corpus-ctx-src q3 (`Merge`), corpus-graphify-src q2 (`node_link_graph`).

Full detail: `~/ctx-bench-arena/quality/coverage.json` (per-cell status),
`~/ctx-bench-arena/quality/context-sizes.json` (per-cell byte size).

## Files

- `~/ctx-bench-arena/results-multi.json` — speed/footprint/query axes (also
  copied to `/Users/muthuishere/muthu/gitworkspace/ctx-optimize/benchmarks/results-multi.json`).
- `~/ctx-bench-arena/logs/` — 174 raw per-command log files for the speed axis.
- `~/ctx-bench-arena/quality/` — 98 raw capture files + `context-sizes.json` +
  `coverage.json` for the (not-yet-judged) answer-quality axis.
- `~/ctx-bench-arena/RUN-NOTES.md` — this file.

## Honesty checklist against the ADR

- Every number is [MEASURED] by a command actually run this session; none
  invented or "reasonable-guessed."
- Every tool timed on its own fastest deterministic path, named above; no
  strawman flags.
- No tool given a fake row: all 4 gather tools + 5 query tools that were
  attempted, succeeded, with real numbers. Serena/potpie omitted with stated
  reasons, not run.
- Secrets stripped from the subprocess env (`KEY`/`TOKEN`/`SECRET`/`PASSWORD`
  substring match, case-insensitive).
- The corpus-graphify-src file-count mismatch vs the ADR's "~12k files"
  assumption is flagged above — do not publish "12k files" without fixing
  that claim first.
- GitNexus does materially more default work (clustering/flows/FTS) than the
  other three tools' timed path; its slower gather numbers should not be
  read as "graph building is 3-5x slower" without that context.
- CodeGraph's query axis used a single keyword, not the same free-text
  question as the others, because its `query` CLI is symbol-search shaped —
  flagged, not hidden.
