# Proposal — graphify gaps → ctx-optimize requirements

## What

ctx-optimize turns a codebase (and adjacent artifacts — SQL, live databases,
messaging schemas) into a queryable knowledge graph + scalable LLM-wiki, so an
AI coding agent finds the right context in a few precise hops instead of burning
tokens on search.

This change captures **graphify's structural gaps** (from market recon + an
owner spike, 2026-07-11) and turns each into a **requirement** ctx-optimize must
satisfy. It is the founding spec: what we build and *why we are different*.

## Why (positioning)

graphify is a proven hit (82K★, 1.2M downloads/mo) but has structural gaps a
focused competitor can win on. We are not out-featuring it; we are fixing the
gaps and shipping in our house style.

**graphify gaps addressed here:**
- **G1 — static file dump, not a live/shareable store.** Emits files; can't be
  shared or synced cleanly (owner's upstream issue #1751 was exactly this).
- **G2 — cloud-model dependence.** Forces LLM calls / cloud providers; heavy
  user demand for local / bring-your-own-model / configurable base URL.
- **G3 — Python & DX friction.** Confusing `graphifyy` name, inline `python -c`
  in the skill, no `uv`, install pain, "overkill for small repos."
- **G4 — imprecise call graph.** tree-sitter + name-based resolution can't
  resolve edges: owner's spike measured **2,487 reliable vs 7,718 guessed** edges
  (1,405 ambiguous) and god-nodes polluted by `get`/`append`/`new`.
- **G5 — fixed `graphify-out/` layout.** Output structure is hardcoded.
- **G6 — weak multi-repo merge.** "How do I combine graphs?" is an open ask.
- **G7 — skill uses inline scripts, not CLI subcommands** (their own issue #12).
- **G8 — solo-maintainer bottleneck.** 242 open PRs, community PRs stale for
  months (positioning, not a functional requirement).

## Scope (this change)

- The **requirements** (SHALL statements) that define the product and its
  differentiation — see `specs/product/spec.md`.
- The **producer-adapter model**: one emit schema, many pluggable adapters.
  Launch adapters: **code** (Go, TS/JS, Python, Rust, Java, C# via go-tree-sitter),
  **SQL**, **live DB introspection**, **messaging schemas**. Adapted from
  graphify's approach (`pg_introspect`, tree-sitter extractors) but **redesigned
  in our style** (Go, `libs/go/<adapter>`, deterministic, DI).
- Delivery is **one target at a time** — each adapter and capability is an
  independently shippable slice behind the stable schema.

## Out of scope (explicitly)

- **No MCP server.** Owner is against MCP. Consumption is an agent skill reading
  the stored artifacts (folder/S3) — never a running server.
- **No LLM inside the binary.** Agent-skill-first; headless via toolnexus with a
  runtime-selected provider.
- Docs/papers/images/video ingest, git merge-driver, Neo4j/FalkorDB/Obsidian
  exports — possible later, not in this founding change.

## Prior art / credit

graphify (MIT, © Safi Shamsi) — read as reference, reimplemented in Go. The
emit-schema-as-contract is proven by graphify's own non-tree-sitter producers
(`scip_ingest.py`, `pg_introspect.py`, `cargo_introspect.py`) emitting the same
`{nodes, edges}`.

## Dependency note (open)

Wiki + storage lean on **citenexus-Go**. Owner's read: "all from golang only —
Go has the Rust capabilities too." A scout is verifying the Go port actually
exposes convert-anything → wiki + folder/S3 storage today; if the rich wiki is
Python-only, that capability is built in citenexus-Go first (dependency ordering).
See `../../docs/VISION.md`.
