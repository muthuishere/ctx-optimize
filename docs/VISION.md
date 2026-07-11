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

## Open architecture questions (to discuss)

1. **Producer strategy at launch** — tree-sitter-first (fast, in-process, broad)
   vs LSP/SCIP-first (exact symbols, needs servers) vs both behind the schema.
   Which languages ship first?
2. **What exactly does ctx-optimize take from citenexus-Go** — the LLM-wiki
   distiller, the document-injection/RAG path, the storage abstraction? Where's
   the seam, and does citenexus-Go expose enough today?
3. **Graph representation & storage** — in-memory + a configurable on-disk
   export (JSON/ndjson?), single-file (SQLite, like CodeGraph) or plain files?
   Live/incremental (watch) vs one-shot build?
4. **The agent-skill contract** — what commands does the skill expose, what does
   it return, how does an agent drive `add/query/path/explain` without a direct
   LLM call in the binary?
5. **The toolnexus headless example** — what does the minimal end-to-end look
   like (skill loaded by toolnexus, citenexus injecting docs, graph queried)?
6. **Configurable output layout** — what's the config surface that replaces
   graphify's fixed `graphify-out/`?
7. **Differentiation wedge** — market recon flagged: live/incremental MCP
   backend, local-first/BYO-model, single Go binary, first-class multi-repo
   merge, responsive maintenance. Which do we lead with?

## Market context (recon 2026-07-11)

graphify: 82K⭐, 1.2M downloads/mo, ~3mo old, solo maintainer, 242 open PRs
(real staleness — community PRs sit for months). Crowded lane: CodeGraph (54K⭐,
MIT, local SQLite + MCP), GitNexus (43K⭐), potpie ($2.2M funded, Neo4j), Serena
(LSP-as-MCP). Winning on features alone is unlikely — differentiate on the wedge
above. graphify's exploitable gaps: static file dump (not live), cloud-model
dependence, Python/DX friction, weak multi-repo merge.
