# ctx-optimize

**Gather a codebase — and its world — into one local knowledge store an AI agent answers from. Deterministic. No LLM API. No DB. Gather once, refresh cheaply, never go everywhere every time.**

ctx-optimize turns a repo (plus, via adapters, database schemas, messaging
topics, log shapes, documents) into a queryable graph stored as plain files in
a central per-module store. Your agent (Claude Code, Codex, Devin — any
skill-capable harness) answers from the store instead of burning tokens on
grep-and-read. The binary never calls a model, a database, or the network —
the only intelligence in the system is the agent you already run.

> ⚠️ **Status: v0.** Working today: code extraction for 9 languages
> (Go, Python, JS, TS/TSX, Java, C, C++, C#, Rust — tree-sitter compiled to
> WASM, zero setup), markdown docs, the universal adapter door, query/path/
> explain/affected/hubs, merge/export, the live dashboard, and remote
> init/push/pull. Symbol cards, the deterministic wiki, and exact call edges
> are next — see `openspec/` for the spike-measured plan.

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
# first time in a repo: scaffold .ctxoptimize/ (config + adapters dir) + the store
ctx-optimize init

# gather a repo into the central store (~/ctxoptimize/<repo-name>/)
ctx-optimize add .

# ask the store — complete, citable hits under a token budget
ctx-optimize query "where is the refund flow" --json

# feed ANY system through the universal adapter door (strictly validated)
./my-postgres-adapter | ctx-optimize add --json -

# combine module stores into one view; dump for other tools
ctx-optimize merge api worker billing --into everything
ctx-optimize export --format dot --out graph.dot

# see it: local dashboard (embedded single file, zero external requests)
ctx-optimize serve          # → http://127.0.0.1:4747 — graph, search, details

# share the store: sync-only remotes (S3-compatible or any folder)
ctx-optimize remote init s3://team-bucket/ctx/myrepo   # writes .ctxoptimize/config.json — commit it
ctx-optimize remote push          # incremental — only changed artifacts move
ctx-optimize remote pull          # a teammate who cloned the repo: this is ALL they run

ctx-optimize status --json
```

- The store is **plain files** (ndjson/json/md) — diffable, portable, at
  `~/ctxoptimize/<repo-name>/`. The only thing in your repo is the
  committable `.ctxoptimize/` directory.
- **Remotes are for sync only.** Queries always run on the local folder.
  `push`/`pull` take no URL — the remote is whatever the config says.

## .ctxoptimize/ — config that travels with the repo

```
.ctxoptimize/
  config.json     name + remote
  adapters/       drop scripts here — every .js/.py/.sh runs on `add`
```

`config.json`:

```json
{
  "name": "my-module",
  "remote": {
    "type": "s3",
    "url": "s3://team-bucket/ctx/my-module",
    "credentials": {
      "access_key_id": "${TEAM_R2_KEY_ID}",
      "secret_access_key": "${TEAM_R2_SECRET}",
      "region": "auto",
      "endpoint": "${R2_ENDPOINT}"
    }
  }
}
```

Commit the directory — it is safe by construction:

- `name` picks the store folder under `~/ctxoptimize/` (default: repo basename).
- `remote` is a plain string URL or the full object above. `${VAR}` anywhere
  in the url/credentials resolves from the environment **at sync time** — the
  file holds variable names, never secret values; resolved values are never
  written or printed. Omitted credentials fall back to the standard `AWS_*`
  env vars (endpoint override covers R2/Hetzner/MinIO).
- **Adapters are files**: dropping `kafka.js` into `.ctxoptimize/adapters/`
  is the whole registration (`.js`/`.mjs` → node, `.py` → python3, `.sh` →
  sh; other extensions inert — `init` seeds an `example.js.sample` template).
  Each script prints batch JSON to stdout; `ctx-optimize add` runs the
  built-in extractors **and** every adapter through the fail-closed door. One
  command refreshes the whole world; a fresh clone needs zero setup to `pull`.

## Adapters — the open door

Everything external is an adapter emitting one JSON schema into
`ctx-optimize add --json -`: nodes (`id`, `label`, `kind`, `file_type`,
`source`, `location`) and edges (`source`, `target`, `relation`,
`confidence` ∈ `EXTRACTED|INFERRED|AMBIGUOUS`). The door validates strictly and
tags provenance per producer. Your agent can write a new adapter on demand —
point it at any system with the schema and it gathers it. Make it permanent by
dropping the script into `.ctxoptimize/adapters/` — every future `add` runs it.

## Design

Evidence-first: every product decision traces to a measured spike
(`openspec/changes/2026-07-11-graphify-gaps/spikes.md`) — including honest
benchmarks against a real agent baseline (not corpus-stuffing strawmen), the
terrain law (graph value is inverse to a codebase's greppability), and the
symbol-card finding (agents' reads are pointer-chases a complete answer
eliminates). Vision: `docs/VISION.md`. Standing critique: `docs/CRITIQUE.md`.

## License

MIT © 2026 Muthukumaran Navaneethakrishnan
