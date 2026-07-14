# Vision & running design notes

## ARCHITECTURE POSITION (2026-07-11, post-Tier-A, maintainer-corrected) —
## deterministic code wiki: graphify's zero-LLM build, Karpathy's FORM, citenexus's discipline

**Maintainer correction (final):** NO LLM work in the product. Not an LLM-distilled
wiki. The core principle stands: no DB, no AI for most of everything. The build
lane is graphify-style — deterministic, seconds-fast, zero tokens (kernel block/
= 2.1s / 0 tokens). The output takes the Karpathy wiki's FORM (index + pages +
[[links]], browsable) but is AUTHORED BY CODE. citenexus contributes its
deterministic machinery: the _deterministic_page concept, the light-index +
pages + append-only-log store layout, and navigate-not-cite grounding (perfect
by construction here — every line is extracted, nothing can be hallucinated).

Why zero-LLM still reduces tokens (spike-grounded):
1. **Symbol cards are 100% deterministic** — signature + doc comment + body head
   + verified file:lines + caller/callee edges. S1e measured cards kill the #1
   waste (28/28 pointer-chase reads). The biggest measured win needs no model.
2. **The distillation already exists — humans wrote it.** Doc comments, READMEs,
   specs, package docs are pre-distilled prose. A page = per-community harvested
   doc-comments + god-node signatures + edge summaries + [[links]] to neighbor
   communities. Organizing existing prose is extraction, not generation.
3. **Exact edges are deterministic** (S3: x/tools VTA batch for Go, persistent-
   LSP driver per language) → `affected` impact analysis, graphify's 81%-recall
   blind spot, grep's impossibility.
4. **Incremental is trivial when deterministic:** member_hash gates regeneration,
   and regeneration is nearly free (no LLM cache economics needed). Seeded-Louvain
   (S4 fix) pins partition stability.

Structure (unchanged from spikes): atom = symbol card, re-verified against
content hashes at answer time (code moves; stale citations worse than none);
page unit = community/subsystem (S4), not file.

**Optional, later, clearly labeled, never the core:** an agent/LLM enrichment
pass over the deterministic wiki (the S1c-style deep pages, 39% measured) —
off by default, cached per member_hash, validated by the binary (every claimed
file:line must exist). LLM proposes, deterministic code disposes.

NOT here: PDF/doc ingest, RAG answering, embeddings — citenexus's proven wedge.
Terrain + positioning: grep-hostile/legacy codebases + onboarding; honest
agent-baseline benchmarks (graphify's strawman 6.6×/70× cannot survive them).

**Open measurement (next spike):** the deterministic composite — zero-LLM wiki +
symbol cards vs grep on the kernel testbed. S1c's 39% was measured on LLM-deep
pages; the deterministic variant's number is unmeasured. It costs nothing to
build (no tokens), so measure before architecture lock.

## REVISION (2026-07-11, later) — citenexus DROPPED from the core; docs-in-the-graph

Maintainer questioned the citenexus dependency; resolution: **not needed in the core.**
citenexus's value is an ANSWERING pipeline (faithfulness gate, cite-or-abstain
generation) — machinery for products that call LLMs to answer. We never answer;
the HOST AGENT answers. We serve context. What remains of the doc lane (extract
→ chunk → nodes → lexical query) we already build for code:
- **Docs are nodes in the SAME graph** (emit schema already has file_type;
  graphify's own markdown extractor proves the pattern, incl. code↔doc edges
  from ADR/NOTE refs). Markdown/txt = deterministic Go producer.
- One graph, one store, one query engine, one wiki, one sync. No Python in the
  core, no install friction (the G3 gap we attack graphify on).
- PDFs/docx: DEFERRED; when needed the SKILL converts (tiny bundled python
  script or the host agent) → markdown → same producer.

### Product qualities (maintainer, 2026-07-11) — fast · answers · zero ceremony · refresh-the-world
1. **graphify-fast is a HARD budget, not a nice-to-have — and Go is chosen to
   BEAT it, multithreaded (maintainer).** graphify's measured effective rate:
   ~48 files/s (Python ProcessPool, 18 workers; k8s 19.6k files = 6m46s).
   Ours: S2 measured 219–537 files/s PER single-threaded wasm worker; one wazero
   instance per goroutine worker (no GIL, no fork overhead) → 10 workers ≈
   ~3,000 files/s → **k8s in ~10–15s (vs their 7min), kernel block/ well under
   1s (vs 2.1s)** — an honest 10–40× build advantage, trivially verifiable by
   anyone with a stopwatch. Multi-module = free parallelism (each module builds
   concurrently, then the merged view). x/tools VTA ~1s/module, parallel. LSP:
   one warm server per language per module, 2–37ms/query, driven concurrently.
   Incremental refresh of a typical edit: content-hash gate → a handful of
   files → milliseconds. Speed compounds the product: at 10s full rebuilds,
   "refresh the world" becomes a habit, not a feature.
2. **It must ANSWER, not point.** The query primitive is the symbol card /
   wiki page — complete, citation-grade, one call (S1e). Pointer lists are the
   graphify mistake we measured.
3. **Zero ceremony.** `ctx-optimize add .` = auto-detect modules + languages,
   build, done. No config file required to start; config only to customize.
4. **`refresh` re-runs the WORLD, incrementally.** The store manifest keeps an
   adapter registry (adapter id, source, last-run, content fingerprint). One
   `ctx-optimize refresh` re-runs every registered producer — tier-1 code/md
   (stat-index + content-hash: only changed files) AND tier-2 adapters (each
   declares its refresh strategy: hash-diff / snapshot-diff / since-timestamp —
   e.g. re-introspect postgres information_schema, diff, update only changed
   tables). Nothing unchanged is recomputed, ever.
5. **The store is the gathered world.** Agents GATHER from the store — code,
   DB schema, messaging topics, log shapes, docs — instead of going to every
   live system every time. Queries never touch live systems; `refresh` keeps
   the store current. One-liner: **gather once, refresh cheaply, answer from
   the store — never go everywhere every time.**

### The open adapter door (maintainer, 2026-07-11) — two producer tiers, one contract
- **Tier 1, compiled into the Go binary (deterministic, zero-dep):** code langs
  (wasm tree-sitter), markdown/txt, exact edges. NOTHING else — no DB drivers,
  no format libs. "No DB" holds literally in the binary.
- **Tier 2, skill-level adapters (scripts, OPEN-ENDED + DYNAMIC):** all
  conversions and live-system introspection — pdf/docx→md; **postgres schema**
  (information_schema → tables/columns/FKs); **messaging middleware schemas**
  (Kafka schema-registry / RabbitMQ definitions / AsyncAPI → topics/events);
  **log structure discovery** (samples → fields/formats/emitters); and unknown
  systems. Three equal sources of adapters:
  1. **bundled** with the skill,
  2. **user-added dynamically** — drop a script into the adapters dir /
     register it; theirs are first-class, they can add whatever,
  3. **agent-written on demand** — the host agent, given the emit schema,
     introspects any system and emits conformant JSON.
- **The universal door:** `ctx-optimize add --json` (stdin/file) accepts the
  emit schema, STRICTLY validated (conformance, dedup, per-adapter provenance
  tag), merged into the one graph. Adapter proposes, binary disposes. The
  binary stays closed and deterministic forever; the adapter surface grows
  without touching it. (Supersedes tasks.md Story 9's "Go libs per adapter" —
  SQL/DB/messaging adapters are skill-level scripts, not compiled code.)
- citenexus = an OPTIONAL skill-level integration for users who already run it;
  its chunker parameters and light-index/pages/log layout are ported as
  PATTERNS, never imported.

## Store layout (maintainer, default) — central, file-based, multi-module
```
~/.ctx-optimize/store/<module-key>/       # user-home central store, keyed by module
  graph/ … wiki/ … cards/ … manifest.json
  hooks/                                  # per-store DYNAMIC adapters (maintainer, 2026-07-11)
    custom_adapter1.py                    #   any executable emitting the node/edge
    postgres_schema.sh                    #   JSON schema on stdout → the validated
    kafka_topics.py                       #   `add --json` door; discovered by `refresh`
```
- **Hooks travel WITH the store** — push/pull shares the adapters too, so a
  teammate pulling the store gets the same gather surface (the #1752 sync-hooks
  idea, made core). Global fallback: `~/.ctx-optimize/hooks/` for all stores.
- **Registered = present:** `refresh` discovers hooks/, runs each (its declared
  refresh strategy), validates output at the door. No registration ceremony.
- **Security (fail-closed):** hooks pulled from a remote are INERT until the
  user approves them (trust-on-first-use, like git hooks never auto-run) —
  pulling a store must never mean executing someone's code silently.
- **Multi-module by default:** detect modules (go.mod / package.json / pom / …)
  → per-module graphs + merged repo view; cross-repo merge on top.
- **Repo stays git-clean** — nothing written into the repo (the #1751/#1752
  central-store design graphify ignored, made the default here).
- **Push/pull anywhere:** plain files + manifest → sync adapters (S3, rsync, …),
  incremental. Layout configurable; user-home is only the default.

## FINAL INTEGRATION CONTRACT (2026-07-11, maintainer) — three layers, "we are not those people"
### (superseded in part by the revision above: citenexus is now OPTIONAL at the skill layer, not a core doc lane)

1. **ctx-optimize binary (Go):** deterministic code engine ONLY — graph, symbol
   cards, zero-LLM wiki, exact edges, store/sync. No LLM calls, no doc-RAG, no
   embeddings, no backends list, no API keys. Ever.
2. **Agent skill = the product surface + orchestration layer.** Drives the binary
   for code. For DOCUMENTS drives **citenexus's Python API — its DETERMINISTIC
   lane only, no embeddings anywhere** (verified 2026-07-11 in both codebases):
   - citenexus signals are independent toggles (`Signal.embedding/lexical/
     structure`, `config/signals.py`; ingest embeds only `if Signal.embedding
     in self._signals`, `ingest/pipeline.py:193`) → configure **embedding OFF**.
   - Retrieval = `LexicalRetriever` / `Bm25TextSearch` — BM25-lite over stored
     text, no vectors (`retrieve/lexical.py`).
   - Pipeline: extract (pdf/docx/html → blocks w/ bbox) → deterministic chunker
     (450/60, no tokenizer dep) → EU store → BM25 lexical → deterministic
     token-overlap faithfulness gate → verbatim cite-or-abstain → deterministic
     wiki pages (light index + pages + log). Zero LLM, zero embeddings, zero
     network.
   **Contrast (why citenexus and not graphify's doc lane):** graphify's
   docs/papers/images/videos REQUIRE a cloud LLM backend (`llm.py` BACKENDS:
   claude/openai/gemini/…) — build-time API calls, non-deterministic, plus LLM
   community naming; its only non-LLM doc path is a regex markdown extractor.
   For documents graphify says "call a cloud LLM"; citenexus says "here is a
   deterministic pipeline with verbatim citations." We consume the latter.
   Semantic work — if ever wanted — is done by the HOST agent (Claude Code /
   Codex / Devin) already running; never an API we call, never embeddings.
3. **citenexus:** grounded document core, consumed at the skill layer only. Per
   its own session's decision (2026-07-11): wires the distiller into
   integrate_document (honesty fix), DEFERS deep distillation, and does not
   double-build — the deep-distill + member_hash cache engine design lives in
   ctx-optimize; citenexus contributes grounding.

The test: graphify is an LLM-API product with a deterministic feature.
ctx-optimize is a deterministic product, full stop — intelligence enters only
through the user's own agent; documents only through citenexus.

**Proof plan ("actual proof everywhere"):** the A/B benchmark protocol ships
with the repo as a repeatable harness — {Claude Code, Codex, Devin} ×
{grep-hostile repos (linux block/ testbed + one legacy monolith)} — real
harness-reported tokens, bare agent vs agent+skill, quality-matched. Claim only
cells the matrix proves (>50% target; measured floor: graphify's inferior
pointer-list version already −23% on kernel C). Honest benchmarks are the
positioning weapon graphify cannot copy.

Living notes for ctx-optimize. **Architecture is under active discussion** — this
file is where decisions and open questions accumulate. Nothing here is frozen.

## The thesis

Today an AI coding agent spends most of its tokens on **code search** — grepping,
reading files, re-reading them. ctx-optimize flips that: build a knowledge graph
of the codebase once, then let the agent answer "where / who-calls / what-breaks"
in a few precise hops. Optimize the context, not the search.

## Intent (from the maintainer, verbatim-ish)

- Works the **same way as graphify**, but uses **citenexus (Go)** as a library
  for the **LLM-wiki** and for **injecting documents**.
- **Exports the same kind of output as graphify**, but **without graphify's
  `graphify-out/` naming structure** — the output layout is **configurable**.
- **Go only.** **Supports all languages** — via a **common interface** (see what
  parsers/indexers are available and plug them in behind one schema).
- **Usable with any agent skill — not direct like graphify.** Agent-skill first.
- **toolnexus** loads agent skills by default → ship a **headless example built
  on toolnexus**.
- **citenexus for injecting documents.**
- **Direct LLM API is never encouraged — agent skill first.** If someone wants
  headless, we hand them a **toolnexus-based example**. That way we stay
  **agnostic**.

## Design decisions carried in from the huddle (2026-07-11)

- **The emit schema is the contract, not the extraction tool.** Producers
  (tree-sitter / LSP / SCIP) conform to one node/edge interface; query stays
  uniform regardless of producer. (graphify already proves this via its
  SCIP/pg/cargo lanes emitting the same `{nodes,edges}`.)
- **Deterministic core.** Graph build + query reproducible/diffable; LLM-flavored
  steps are the agent's job.
- **Don't pollute citenexus.** It stays a focused RAG-with-citations library;
  ctx-optimize consumes it. (This is why this is its own repo, not a citenexus
  change.)
- graphify is **MIT** — reuse as reference freely; reimplement in Go.

## Round 2 decisions (2026-07-11) — the maintainer's calls

- **NO MCP.** Firm — we do not ship an MCP server. (Maintainer is against MCP on
  principle.) This removes the "live MCP backend" wedge entirely.
- **Consumption = S3-hosted wiki + an agent skill over a token-auth URL.** The
  LLM-wiki/graph lives in **S3 as parquet** (or a columnar/Lance format). We ship
  an **agent skill** configured with that **S3 URL + a token**; the agent (or
  toolnexus headless) drives `ctx-optimize` commands that read from S3. Sharing =
  hand over the URL + token. This is the #1751 "central shareable store" idea
  realized **without a server** — the non-MCP answer to graphify's static dump.
- **citenexus = the Go library ONLY** (maintainer: "all from golang only — Go has the
  Rust capabilities too"). No Rust/cgo split. ctx-optimize depends on
  **citenexus-Go** for:
  1. **"convert anything → wiki"** + the **scalable LLM-wiki** that syncs to a
     **folder or S3**.
  2. **Graph storage** — its folder-level / S3-level / multi-level backends.
  (Confirm the Go API actually exposes both when we reach that phase — light
  check, not a blocker; proceeding on the maintainer's read.)
- **Extraction = Go + go-tree-sitter.** Launch languages: **Go, TS/JS, Python,
  Rust, Java, C#** (6). Broaden later behind the same emit schema.
- **Storage = plain, syncable artifacts in folder or S3** (via citenexus-Go),
  parquet/columnar for the scalable wiki. Matches the dir-as-database idiom.
- **citenexus will NOT host code-graph** (maintainer decision, recorded in
  citenexus/INPROGRESS). Code lives here.

### Maintainer's spike finding (load-bearing) — tree-sitter edges are imprecise
A throwaway Rust tree-sitter spike over the citenexus repo (352 files / 4 langs /
~1,900 symbols) showed name-based resolution cannot give a trustworthy call graph:
**2,487 reliable vs 7,718 guessed edges**, 1,405 ambiguous sites, god-nodes
polluted by name collisions (`get`/`append`/`new`). This is graphify's hidden
weakness too. Implications for ctx-optimize:
- We are **not** cite-or-abstain (that constraint was citenexus's), so
  **confidence-tiered edges are acceptable** — emit EXTRACTED / INFERRED /
  AMBIGUOUS and let the agent weigh them (graphify-parity behaviour).
- BUT **precise edges are the real differentiator.** A per-language **LSP/SCIP**
  producer gives exact references/call-hierarchy that tree-sitter name-resolution
  only guesses. tree-sitter first (broad, cheap, honest confidence tiers); LSP/SCIP
  where precision wins — same emit schema behind both. This is the wedge, not MCP.

## Open architecture questions (to discuss)

1. **PIVOTAL — verify citenexus-Go actually exposes wiki + storage.** Maintainer's read
   is "all from golang only — Go has the Rust capabilities too." Confirm the Go
   port exports (a) a convert-anything → wiki / LLM-wiki, and (b) folder/S3
   (ideally parquet/Lance) storage a Go program can import. Earlier recon
   suggested the rich wiki was **Python-only** and Go carried mainly core/gate +
   LanceStore — if so, "wiki via citenexus-Go" means building it in citenexus-Go
   first (dependency ordering). *(Scout running 2026-07-11.)*
2. **The parquet/columnar wiki schema in S3** — what's stored (nodes, edges,
   wiki pages, embeddings?), in what columnar layout, and how does `query` read
   it efficiently from S3 without pulling everything?
4. **The agent-skill contract** — commands the skill exposes, `--json` shapes,
   how it passes the S3 URL + `--token-env`, all with no LLM call in the binary.
5. **The toolnexus headless example** — minimal end-to-end: skill loaded by
   toolnexus → `ctx-optimize query --store s3://… --token-env …` → compact context.
6. **Configurable output layout** — the config surface that replaces graphify's
   fixed `graphify-out/` (folder vs S3, path templates).
7. **Build vs query split** — `add`/build writes graph+wiki to folder/S3; `query`
   reads from there. Incremental re-index (watch) vs one-shot — still open, but
   **without** MCP either way.

## Market context (recon 2026-07-11)

graphify: 82K⭐, 1.2M downloads/mo, ~3mo old, solo maintainer, 242 open PRs
(real staleness — community PRs sit for months). Crowded lane: CodeGraph (54K⭐,
MIT, local SQLite + MCP), GitNexus (43K⭐), potpie ($2.2M funded, Neo4j), Serena
(LSP-as-MCP). Winning on features alone is unlikely — differentiate on the wedge
above. graphify's exploitable gaps: static file dump (not live), cloud-model
dependence, Python/DX friction, weak multi-repo merge.
