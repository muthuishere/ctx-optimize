# Vision & running design notes

Living notes for ctx-optimize. **Architecture is under active discussion** — this
file is where decisions and open questions accumulate. Nothing here is frozen.

## The thesis

Today an AI coding agent spends most of its tokens on **code search** — grepping,
reading files, re-reading them. ctx-optimize flips that: build a knowledge graph
of the codebase once, then let the agent answer "where / who-calls / what-breaks"
in a few precise hops. Optimize the context, not the search.

## Intent (from the owner, verbatim-ish)

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

## Round 2 decisions (2026-07-11) — the owner's calls

- **NO MCP.** Firm — we do not ship an MCP server. (Owner is against MCP on
  principle.) This removes the "live MCP backend" wedge entirely.
- **Consumption = S3-hosted wiki + an agent skill over a token-auth URL.** The
  LLM-wiki/graph lives in **S3 as parquet** (or a columnar/Lance format). We ship
  an **agent skill** configured with that **S3 URL + a token**; the agent (or
  toolnexus headless) drives `ctx-optimize` commands that read from S3. Sharing =
  hand over the URL + token. This is the #1751 "central shareable store" idea
  realized **without a server** — the non-MCP answer to graphify's static dump.
- **citenexus = the Go library ONLY** (owner: "all from golang only — Go has the
  Rust capabilities too"). No Rust/cgo split. ctx-optimize depends on
  **citenexus-Go** for:
  1. **"convert anything → wiki"** + the **scalable LLM-wiki** that syncs to a
     **folder or S3**.
  2. **Graph storage** — its folder-level / S3-level / multi-level backends.
  (Confirm the Go API actually exposes both when we reach that phase — light
  check, not a blocker; proceeding on the owner's read.)
- **Extraction = Go + go-tree-sitter.** Launch languages: **Go, TS/JS, Python,
  Rust, Java, C#** (6). Broaden later behind the same emit schema.
- **Storage = plain, syncable artifacts in folder or S3** (via citenexus-Go),
  parquet/columnar for the scalable wiki. Matches the dir-as-database idiom.
- **citenexus will NOT host code-graph** (owner decision, recorded in
  citenexus/INPROGRESS). Code lives here.

### Owner's spike finding (load-bearing) — tree-sitter edges are imprecise
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

1. **PIVOTAL — verify citenexus-Go actually exposes wiki + storage.** Owner's read
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
