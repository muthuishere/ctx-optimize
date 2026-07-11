# Spikes — prove/kill assumptions BEFORE architecture

Throwaway experiments (scratch code lives outside the repo; only RESULTS land
here). Order: **S1 alone first** (validates the thesis), then S2–S5 in parallel,
Tier B before design lock, Tier C during build. Architecture discussion happens
AFTER Tier A reports.

Context: no embeddings · no DB · no AI in the core · S3 = sync only · everything
external is an adapter.

## Tier A — existential

### S1 · Token economics (THE thesis)
- **Question:** does a graph+wiki actually beat grep/read for agent questions?
- **Method:** 10 real questions on a known repo. Agent A answers via grep/read
  only; Agent B answers via `graphify query` only (graphify as proxy for our
  product). Count real agent tokens + judge answer quality.
- **Pass:** >50% token reduction at equal quality. **Fail ⇒ stop the product.**
- **Result (2026-07-11): ❌ FAIL on a mid-size repo — only ~11% reduction.**
  brain repo (~350 files; graph 662 nodes / 1,252 edges / 38 communities, no-LLM
  lane). Grep agent: 56,114 tok / 15 calls / 10-10 correct (sharpest). Graph
  agent: 49,826 tok / 22 calls / 10-10 correct, 1–2 minor imprecisions.
  **Why:** mechanism questions still require reading code bodies — the graph
  locates but doesn't explain, so the agent pays for queries AND reads; modern
  grep agents are already efficient at this scale; the wiki layer (the actual
  pre-distilled understanding) wasn't present.
  **Surviving hypotheses → S1b:** (a) large repos (10k+ files) where grep
  flails; (b) the LLM wiki as the token-saver (bare graph is just the index);
  (c) relationship queries (`affected`/`path`) grep cannot do at all — impact
  analysis may be the real product, not Q&A.
  **Implication for the product:** do NOT lead with "graph query saves tokens on
  your repo." Lead with what grep can't do + the wiki. Re-test before building.

### S1b · Token economics at scale + wiki layer (follow-up, REQUIRED)
- **Method:** repeat S1 on a genuinely large repo (10k+ files); and separately
  test Q&A against a distilled wiki (pages pre-built) vs grep.
- **Pass:** same >50% bar, on either the scale axis or the wiki axis.
- **Build-scale data (kubernetes, 2026-07-11):** 13,134 Go files (17.5k w/
  vendor; 19.6k ingested) → **286,616 nodes / 622,685 edges / 15,076
  communities** in **6m46s**, peak **2.8 GB RAM**, zero LLM tokens. Edge
  provenance 83% EXTRACTED / 17% INFERRED (avg conf 0.8).
  **Design lessons:** `graph.json` = **360 MB single blob** — validates our
  columnar/sharded-manifest storage requirement; **15k communities = 15k wiki
  pages** — the wiki needs hierarchy/pruning (god-communities, size thresholds)
  at scale; HTML viz refuses >5k nodes (their own tool can't render its output).
- **A/B result (2026-07-11): ❌ DECISIVE FAIL — graphify cost MORE than grep at
  scale.** Grep: 32,546 tok / 22 calls / 116s / 10-10 sharpest. Graphify:
  42,800 tok / 29 calls / 385s / 10-10 but `affected` on PodSpec BLOCKED by
  node-ID name collision, several answers less precise. **+31% tokens, 3.3×
  slower.** Grep got CHEAPER at scale (32.5k vs 56k on the mid repo) — well-named
  code is its own index.

## S1 OVERALL VERDICT — the "graph query saves tokens" thesis is FALSIFIED
Mid repo: −11%. Large repo: +31% (negative). Both quality-matched. Do NOT build
a product on "cheaper code search for agents."
**Surviving hypotheses (the only ones):**
1. **Incremental LLM wiki** — pre-distilled understanding answering conceptual
   questions with zero search (S1c to test; S4 already proved the incremental
   machinery viable).
2. **Precise impact analysis** — `affected`/`path` with LSP-grade edges (S3):
   the query class grep cannot do, and where graphify demonstrably breaks
   (name collisions at 287k nodes).
**Cancelled:** S5 (lexical query engine port) — pointless to port an engine that
loses to grep.

### S1c · Wiki-axis test (NEW — the last token-saving hypothesis)
- **Method:** brain repo (38 communities): label + distill a wiki cheaply, then
  10 CONCEPTUAL questions (why/how-does-it-fit-together, not where-is-X):
  grep agent vs wiki-reading agent. Same bar.
- **Result:** _pending_

### S1d · The kernel test (owner challenge — "Linux showed progress, why?")
- **Owner's counter-evidence:** his graphify run on Linux kernel block/ showed
  progress. Reconciliation hypothesis — S1/S1b tested the two most grep-friendly
  codebases on earth (modern Go, predictable naming); **the graph pays off where
  naming fails**: legacy C/C++, macros, function pointers, huge files, obscure
  symbol→file mapping. Also: graphify's own benchmark compares against naive
  full-file reading, not a strong grep agent — different baseline, both honest.
- **Method:** same A/B protocol on linux/block (C). Plus run graphify's own
  `benchmark` to capture what baseline IT uses.
- **Pass:** >50% cut on kernel-style code ⇒ the product's territory is
  "codebases where grep fails" (legacy/C/monoliths) — a real, large market.
- **Benchmark forensics (2026-07-11) — WHY graphify "shows progress":** its
  self-reported "6.6×" is a strawman, confirmed in `benchmark.py`: baseline =
  paste-the-ENTIRE-corpus-per-query (nobody does this); corpus size not even
  measured (estimated `nodes × 50` words); the "query cost" side is an
  unbudgeted depth-3 BFS dump (~29k tok) that ignores its own `--budget 2000`
  default; 2 of 5 canned questions matched nothing and were silently dropped.
  Marketing number, not agent-reality. Our A/B replaces it.
- **Build data:** block/ = 98 files / 73k lines of C → 2,854 nodes / 7,081
  edges / 133 communities in **2.1s**, zero LLM. (Fresh 0.9.12 `update` builds
  still emit pre-#1504 colliding node IDs — their own fix isn't wired into the
  no-LLM lane.)
- **A/B result (2026-07-11): ⚠️ FIRST GRAPH WIN — ~23% cheaper on kernel C.**
  Grep: 45,775 tok / 18 calls / 10-10. Graph: 35,414 tok / 13 calls / 10-10
  (self-diagnosed the ambiguous submit_bio node and traced around it).
  **The terrain law (across all three A/Bs):** graph value is inversely
  proportional to greppability — brain (clean Go): graph −11%; k8s (hyper-
  greppable): graph +31% WORSE; kernel C (opaque naming): graph −23%. It's not
  size, it's whether names betray structure. Owner's Linux observation
  CONFIRMED directionally.
  **But 23% < the 50% bar even on best terrain.** The bare pointer-list query
  is a modest win, not a product-carrying one. Identified lever: symbol-grade
  COMPLETE answers (signature + body snippet + verified lines in the query
  output) to eliminate the follow-up pointer-chase reads — S1e quantifies how
  much of B's residual cost those reads are.

### S2 · Pure-Go extraction (single-binary promise)
- **Question:** can we extract without cgo?
- **Method:** same 2 grammars three ways — official go-tree-sitter (cgo) vs
  wazero+WASM grammars (pure Go) vs subprocess SCIP indexer. Build for
  mac/linux/windows; measure binary size + parse speed.
- **Pass:** 6 launch languages feasible with a sane release matrix.
- **Result (2026-07-11): ✅ PASS — wazero + self-built WASI wasm module.**
  Measured: cgo fails hard under CGO_ENABLED=0 (only works via zig cc);
  the one pure-Go runtime (gotreesitter) is DISQUALIFIED (>300s hang on a real
  215KB file that cgo parses in 25ms; 0.6% symbol loss). The spike BUILT the
  missing wazero binding (~80-line C shim + ~150-line host): all 7 grammars
  (go/py/js/ts/rust/java/c#) in one 11MB embedded wasm, symbols byte-identical
  to cgo, ~55–60% cgo speed (219–537 f/s ⇒ ~10k files/45s single-threaded),
  full cross-compile matrix with plain `go build`, ~118ms one-time JIT.
  **Decision: Route B-2.** .scm queries stay the extraction language. CI owns a
  zig/wasi-sdk wasm pipeline (build-time only). Fallbacks: cgo+zig cc if speed
  ever demands; ast-grep (MIT) as optional external accelerator, never required.
  **Resolves review finding R1 unamended** — single static binary holds.
  Versions: go-tree-sitter v0.25.0 grammars, wazero v1.12.0, zig 0.15.2.

### S3 · Precise edges (the wedge)
- **Question:** does LSP/SCIP fix the 2,487-reliable-vs-7,718-guessed problem?
- **Method:** drive `gopls` call-hierarchy + parse `scip-go` on the same repo as
  the owner's spike; compare edge precision vs tree-sitter name-resolution.
- **Pass:** ≥90% precision on sampled edges; runs headless.
- **Result:** _pending_

### S4 · Community stability (incremental design lives/dies here)
- **Question:** does a one-file edit repartition the whole graph?
- **Method:** build graph on a mid repo, partition (Louvain), edit 5 files one at
  a time with community-ID remapping; measure % pages whose member_hash flips.
- **Pass:** single-file edit touches ≤10% of communities.
- **Result (2026-07-11): ⚠️ MARGINAL — design VIABLE with a fatter tail.**
  citenexus-Go copy (239 nodes / 413 edges / 17 communities). Body edit 0%,
  small add 5.9%, rename 5.9% ✅ · delete-function 17.6%, add-caller-of-hub
  23.5% ❌. Crucially: NEVER a global repartition — IDs stable across all runs
  (remap works), no-op rerun = 0% (deterministic topology-hash short-circuit).
  Failures are bounded local cascades: 2–4 boundary clusters migrate when an
  edge lands on a high-degree hub; Jaccard-thresholding does NOT rescue (real
  3–7-node migrations).
  **Design implication:** member_hash re-distill works — common case 0–1 pages,
  budget a burst of ~2–4 pages on hub-adjacent edits. To kill the tail: seed
  Louvain with the previous partition (frozen boundaries) — partitioner-side
  fix, adopt in our Go implementation (S6).

### S5 · Lexical query engine (it IS the engine — no embeddings)
- **Question:** does IDF+trigram+budget-BFS in Go match graphify's answers?
- **Method:** port graphify's scoring to a Go prototype; same questions, same
  repo; compare selected nodes + latency.
- **Pass:** comparable node selection; <100ms local.
- **Result:** _pending_

## Tier B — architecture-shaping

### S6 · Louvain/Leiden in Go
gonum/graph/community deterministic with seed control? **Pass:** identical
partition across runs. **Result:** _pending_

### S7 · Per-community distill
One community → one useful wiki page; prompt fit; deterministic fallback with no
LLM. **Pass:** useful pages, clean degradation. **Result:** _pending_

### S8 · Incremental reconcile + ripple
Evict+re-resolve incl. new-symbol-makes-old-name-ambiguous. **Pass:** incremental
graph ≡ full rebuild. **Result:** _pending_

### S9 · Sync + share UX
Push to R2/S3; teammate pulls with scoped token; queries locally. **Pass:** fresh
machine answering in <5 min. **Result:** _pending_

### S10 · Manifest/columnar at scale
100k-node store: JSON vs parquet locally; incremental push moves only changed
artifacts. **Pass:** local query <100ms; push = changed files only.
**Result:** _pending_

## Tier C — during build (adapters + integration)

- **S11** SQL adapter (tree-sitter-sql / pg_query_go on DDL) — _pending_
- **S12** Live DB introspection (pgx → information_schema) — _pending_
- **S13** Messaging-queue schemas (pick protobuf/Avro/AsyncAPI first) — _pending_
- **S14** Redis keyspace adapter (SCAN+TYPE → shape) — _pending_
- **S15** SKILL.md drive test (agent drives subcommands cleanly) — _pending_
- **S16** toolnexus headless (`run --once`, model-free binary) — _pending_

## Killed by decisions (no spike needed)
- Vector store / embeddings comparisons — owner: no embeddings, ever.
- Lance/Postgres query engines — owner: no DB.
- S3 live-read latency — owner: S3 is sync-only, queries are local.
