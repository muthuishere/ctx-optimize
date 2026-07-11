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
- **A/B result:** _pending (agents running)_

### S2 · Pure-Go extraction (single-binary promise)
- **Question:** can we extract without cgo?
- **Method:** same 2 grammars three ways — official go-tree-sitter (cgo) vs
  wazero+WASM grammars (pure Go) vs subprocess SCIP indexer. Build for
  mac/linux/windows; measure binary size + parse speed.
- **Pass:** 6 launch languages feasible with a sane release matrix.
- **Result:** _pending_

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
