# Tasks — one target at a time

Ordered so each item is an independently shippable slice behind the stable emit
schema. Check off as done; each substantive slice ends on a green `task check`.
**Targeted, not broad** — we do not chase graphify's full surface.

## Story 0 — scaffold (house style)
- [ ] `clis/go/ctx-optimize` thin `main` (switch dispatch, hand-rolled flags, `--json`, `usage`)
- [ ] Taskfile v3 with the `check` gate (`vet` + `build` + `test`)
- [ ] `.github/workflows/ci.yml` (PR+push, SHA-pinned, `go-version-file`) + `release.yml` (cross-compile matrix)
- [ ] `openspec/` wired; this change is the first spec

## Story 1 — the contract (load-bearing)
- [ ] `libs/go/graph`: `Node`/`Edge` types with confidence tiers; JSON schema is THE contract
- [ ] Producer interface: `Produce(input) ([]Node, []Edge, error)` — everything conforms

## Story 2 — first code adapter (tree-sitter, one language)
- [ ] `libs/go/extract/treesitter`: Go language first, emits the schema
- [ ] god-node computation excluding noise labels; confidence tiers on edges

## Story 3 — query core
- [ ] `query` / `path` / `explain` over the in-memory graph, `--json`
- [ ] hermetic tests (`t.TempDir`, `captureStdout`)

## Story 4 — storage (build; reuse citenexus-Go where apt)
- [ ] `libs/go/store`: folder + S3 artifact backend + manifest (NOT in citenexus-Go — build it)
- [ ] configurable layout (no fixed output dir); optionally parquet/columnar; reuse citenexus-Go Lance/pgvector for the vector index only

## Story 5 — incremental wiki
- [ ] content-hash stat/cache index (invalidate changed units only)
- [ ] wiki generation on `models.*` LLM client (injected), per-community pages
- [ ] incremental integrate (port citenexus-Python `WikiStore.integrate_document`): re-distill only affected pages
- [ ] wiki + graph sync to folder/S3

## Story 6 — agent skill first
- [ ] `skills/ctx-optimize/SKILL.md` (frontmatter router + intent→subcommand table, `--json`, no inline scripts)
- [ ] `//go:embed` + `install-skills` (writes skill, seeds `~/.config/ctx-optimize/config.json`)
- [ ] skill reads store via URL + `--token-env`

## Story 7 — headless via toolnexus
- [ ] `examples/headless/` — toolnexus loads the skill, `run --once "…"`; provider at runtime, model-free binary

## Story 8 — precision (better than graphify)
- [ ] LSP/SCIP producer for code (EXTRACTED-grade edges) behind the same schema; tree-sitter stays the broad/cheap path

## Story 9 — more adapters (targeted, one at a time)
- [ ] remaining code languages: TS/JS, Python, Rust, Java, C#
- [ ] SQL adapter (DDL/schema files)
- [ ] live DB introspection adapter (adapt graphify `pg_introspect`, our design)
- [ ] messaging-schema adapter (event/message schemas → graph)

## Story 10 — multi-repo merge
- [ ] merge graphs across repos with dedup + repo tagging

## Deferred (explicitly NOT in this change — stay targeted)
- Docs / papers / images / video ingest (graphify breadth we are not chasing)
- Exports: Obsidian / HTML / SVG / GraphML / Neo4j / FalkorDB
- git merge-driver, global cross-repo store
- MCP — **never** (maintainer decision)
