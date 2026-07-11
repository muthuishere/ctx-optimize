# Spec — ctx-optimize product requirements

## Purpose

Define what ctx-optimize is and how it beats graphify on the gaps in `proposal.md`.
It builds a **targeted, expandable, scalable** code-knowledge graph + **incremental**
LLM-wiki that an agent queries to spend fewer tokens on code search. **Focused, not
broad** — we target high-value adapters and make every piece coherent, rather than
covering everything graphify does.

## Design principles

- **Targeted, not broad.** A small, coherent core + a curated set of adapters —
  not graphify's 40-language / docs / images / video sprawl.
- **Expandable.** One emit schema is the contract; new producers/adapters plug in
  behind it without touching the core.
- **Scalable.** Artifacts live in a folder or S3; work is **incremental** — only
  changed units are recomputed, the rest is read from the store.
- **Better than graphify.** Precise edges (LSP/SCIP, not just name-resolution),
  incremental wiki, syncable shareable store, single Go binary.
- **House style.** Go, `clis/go/ctx-optimize` thin + `libs/go/<pkg>`, hand-rolled
  dispatch, `--json`, deterministic, DI, offline default. Spec-driven, one target
  at a time.

## citenexus-Go reuse map (verified 2026-07-11)

- **Reuse from citenexus-Go:** deterministic kernel (`answer`, `gate`, `bm25`,
  `rrf`, `chunker`, `ingest`, `tokenize`, `lang`), `graph.BuildComentionGraph`
  (build-only), `models.*` (LLM wire clients), vector store (`LanceVectorStore`
  → folder/`s3://` via CGo, or pure-Go `PostgresVectorStore`).
- **Build ourselves (NOT in citenexus-Go — Python-only today):** LLM-wiki
  generation + incremental integration; folder/S3 **artifact** storage
  (`StorageBackend`/local/S3 + manifest + parquet); graph query/path/persist.
  Port from citenexus-Python (`wiki/store.py::integrate_document`, `graph/store.py`)
  or write fresh on `models.*`.

## Requirements

### Requirement: Single Go binary, no runtime (addresses G3)
ctx-optimize SHALL ship as a single statically-linked Go binary installable without
a language runtime or package manager.

#### Scenario: install without a runtime
- **GIVEN** a machine with no Python/Node
- **WHEN** the user downloads the ctx-optimize binary (or `go install`s it)
- **THEN** all core commands run with no additional runtime or dependency install

### Requirement: Model-free binary, agent-skill first (addresses G2)
The binary SHALL make no LLM/network call to do its work. Reasoning is the agent's
job via a bundled SKILL.md; headless runs use toolnexus with a runtime-selected
provider. Any LLM step (wiki distill) SHALL take an injected client, key by env-var
name, never baked in.

#### Scenario: offline core
- **GIVEN** no network and no configured provider
- **WHEN** the user runs `add` / `query` / `path` / `explain`
- **THEN** they succeed against local artifacts with no outbound call

### Requirement: No MCP; consumption via agent skill over the store (owner decision)
ctx-optimize SHALL NOT ship or require an MCP server. Consumption is an agent skill
that drives CLI subcommands reading the stored artifacts (folder or S3 URL + token).

#### Scenario: shared graph over a token URL
- **GIVEN** a graph+wiki published to `s3://…` with an access token
- **WHEN** a teammate's agent loads the skill configured with that URL + `--token-env`
- **THEN** it answers queries from the store with no server running anywhere

### Requirement: Syncable, shareable folder/S3 artifact store (addresses G1; owner #1751)
Graph + wiki artifacts SHALL persist to a folder or S3 as syncable files (parquet/
columnar where it helps), shareable by handing over the URL + token. A manifest
SHALL record what is current so sync and incremental work are possible.

#### Scenario: sync instead of rebuild
- **GIVEN** a store already built and pushed to S3
- **WHEN** another machine points ctx-optimize at that URL
- **THEN** it reads the existing graph+wiki without re-extracting from source

### Requirement: Configurable output layout (addresses G5)
The output location and layout SHALL be configurable (folder vs S3, path template).
There SHALL be no hardcoded `graphify-out/`-style directory.

#### Scenario: custom layout
- **GIVEN** a config setting the store root and path template
- **WHEN** the user runs `add`
- **THEN** artifacts are written to the configured location, not a fixed dir

### Requirement: One emit schema, pluggable producer adapters (the contract; expandability)
All producers SHALL emit ONE schema:
`node = {id, label, kind, source, location, +metadata}`,
`edge = {source, target, relation, confidence, +metadata}`.
Adding a producer SHALL NOT require changing the graph/query/wiki core.

#### Scenario: new adapter, unchanged core
- **GIVEN** a new adapter emitting the schema
- **WHEN** it is registered
- **THEN** query/path/explain/wiki work over its output with no core change

### Requirement: Targeted producer adapters, one at a time
Launch adapters, delivered independently: **code** (Go, TS/JS, Python, Rust, Java,
C# via go-tree-sitter), **SQL**, **live DB introspection**, **messaging schemas**.
Each SHALL be adapted from graphify's approach but redesigned in our style (Go,
`libs/go/<adapter>`, deterministic).

#### Scenario: DB adapter
- **GIVEN** a database DSN (`--token-env` for creds)
- **WHEN** the user runs the DB adapter
- **THEN** tables/columns/keys/relations are emitted as schema nodes+edges into the graph

### Requirement: Confidence-tiered + precise edges (addresses G4; the differentiator)
Edges SHALL carry a confidence tier (EXTRACTED / INFERRED / AMBIGUOUS) and god-nodes
SHALL exclude noise labels (`get`/`new`/…). Precise edges SHALL be available via an
optional per-language **LSP/SCIP** producer, behind the same schema. Both tree-sitter
(broad, cheap) and LSP (precise) are required and interchangeable.

#### Scenario: precise mode
- **GIVEN** a language server available for a repo
- **WHEN** the user builds with the LSP producer
- **THEN** call edges are EXTRACTED-grade (exact references), not name-guessed

### Requirement: Incremental, scalable wiki (owner: "incremental like graphify, better")
Building or rebuilding SHALL be incremental. Invalidation SHALL key on **content
hash** (mtime/size only as a fast-path gate), with separate structural vs LLM hashes
so a structural change never re-bills the LLM. The re-distill unit SHALL be the
**community/cluster**, keyed by a `member_hash`; a page re-distills only when its
member set changes, and cache hits (unversioned distill cache) avoid the LLM call.
Storage SHALL be per-page objects + a light manifest so query reads only the
manifest. See `design.md` for the full mechanism.

#### Scenario: one file changes
- **GIVEN** a built store and one edited source file
- **WHEN** the user re-runs `add`
- **THEN** only that file re-extracts, only the communities whose `member_hash`
  changed re-distill (others are read from the store), and no whole-corpus LLM call
  or global wiki rewrite occurs

#### Scenario: release does not re-bill
- **GIVEN** a new ctx-optimize version with an unchanged distiller prompt
- **WHEN** the user re-runs `add` on an unchanged repo
- **THEN** the unversioned distill cache hits for every community — zero LLM calls

### Requirement: First-class multi-repo merge (addresses G6)
ctx-optimize SHALL merge graphs across repos into one, with node dedup and repo
tagging to avoid same-name collisions.

#### Scenario: combine two repos
- **GIVEN** graphs for repo A and repo B
- **WHEN** the user merges them
- **THEN** one graph results, symbols deduped, cross-repo edges preserved

### Requirement: Skill uses CLI subcommands, not inline scripts (addresses G7)
The agent skill SHALL drive documented `ctx-optimize` subcommands with `--json`
output — never inline interpreter code.

#### Scenario: skill call
- **GIVEN** the installed skill
- **WHEN** the agent needs an answer
- **THEN** it runs `ctx-optimize query --json …` and parses structured output
