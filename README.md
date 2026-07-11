# ctx-optimize

**Gather a codebase — and its world — into one local knowledge store an AI agent answers from. Deterministic. No LLM API. No DB. Gather once, refresh cheaply, never go everywhere every time.**

ctx-optimize turns a repo (plus, via adapters, database schemas, messaging
topics, log shapes, documents) into a queryable graph stored as plain files in
a central per-module store. Your agent (Claude Code, Codex, Devin — any
skill-capable harness) answers from the store instead of burning tokens on
grep-and-read. The binary never calls a model, a database, or the network —
the only intelligence in the system is the agent you already run.

> ⚠️ **Status: v0 under construction.** The store, the universal adapter door,
> markdown extraction, lexical query, and remote init/push/pull work today.
> Code-language extraction (tree-sitter/WASM), symbol cards, the deterministic
> wiki, and exact call edges are landing next — see `openspec/` for the
> spike-measured plan.

## Install

npm (recommended — thin JS launcher resolves a prebuilt platform binary via
`optionalDependencies`; no postinstall script, no download):

```sh
npm install -g @muthuishere/ctx-optimize
```

Go:

```sh
go install github.com/muthuishere/ctx-optimize/cmd/ctx-optimize@latest
```

Then install the agent skill (writes to `~/.claude/skills`, and
`~/.agents/skills` when codex is present):

```sh
ctx-optimize install --skills
```

## Usage

```sh
# gather a repo into the central store (~/.ctx-optimize/store/<module-key>/)
ctx-optimize add .

# ask the store — complete, citable hits under a token budget
ctx-optimize query "where is the refund flow" --json

# feed ANY system through the universal adapter door (strictly validated)
./my-postgres-adapter | ctx-optimize add --json -

# share the store: sync-only remotes (S3-compatible or any folder)
ctx-optimize remote init s3://team-bucket/ctx/myrepo
ctx-optimize remote push          # incremental — only changed artifacts move
ctx-optimize remote pull          # teammate pulls, queries locally

ctx-optimize status --json
```

- The store is **plain files** (ndjson/json/md) — diffable, portable; your repo
  is never written to.
- **Remotes are for sync only.** Queries always run on the local folder.
- S3 credentials come from the standard env vars at call time
  (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`,
  `AWS_ENDPOINT_URL` for R2/Hetzner/MinIO) — never stored.

## Adapters — the open door

Everything external is an adapter emitting one JSON schema into
`ctx-optimize add --json -`: nodes (`id`, `label`, `kind`, `file_type`,
`source`, `location`) and edges (`source`, `target`, `relation`,
`confidence` ∈ `EXTRACTED|INFERRED|AMBIGUOUS`). The door validates strictly and
tags provenance per producer. Adapters live in the store's `hooks/` dir and
travel with push/pull. Your agent can write a new adapter on demand — point it
at any system with the schema and it gathers it.

## Design

Evidence-first: every product decision traces to a measured spike
(`openspec/changes/2026-07-11-graphify-gaps/spikes.md`) — including honest
benchmarks against a real agent baseline (not corpus-stuffing strawmen), the
terrain law (graph value is inverse to a codebase's greppability), and the
symbol-card finding (agents' reads are pointer-chases a complete answer
eliminates). Vision: `docs/VISION.md`. Standing critique: `docs/CRITIQUE.md`.

## License

MIT © 2026 Muthukumaran Navaneethakrishnan
