# ctx-optimize

[![CI](https://github.com/muthuishere/ctx-optimize/actions/workflows/ci.yml/badge.svg)](https://github.com/muthuishere/ctx-optimize/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/@muthuishere/ctx-optimize?logo=npm)](https://www.npmjs.com/package/@muthuishere/ctx-optimize)
[![benchmark](https://img.shields.io/badge/benchmark-run%20it%20yourself-4ade80)](https://muthuishere.github.io/ctx-optimize-site/proof/agent/)
[![platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-blue)](https://www.npmjs.com/package/@muthuishere/ctx-optimize)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Gather a codebase — and its world — into one local knowledge store an AI agent answers from. Deterministic. No LLM API. No DB. Gather once, refresh cheaply, never go everywhere every time.**

Your coding agent burns its context window on grep-and-read: to answer one
question it greps, opens files, chases callers, re-reads. ctx-optimize turns a
repo — plus, via adapters, database schemas, messaging topics, log shapes,
documents — into a queryable graph stored as plain files in a central
per-module store, and your agent (Claude Code, Codex, Devin — any skill-capable
harness) answers *from the store* in a single call. The binary never touches a
model, a database, or the network: it's deterministic, and the only
intelligence in the system is the agent you already run.

> **Status: v0.1.x — published.** On npm (`@muthuishere/ctx-optimize`) with
> prebuilt binaries for macOS / Linux / Windows; CI green; benchmarks
> reproducible (see [Proof](#proof--reproducible-not-our-word)). Working
> today: code extraction for **12 embedded languages** (Go, Python, JS,
> TS/TSX, Java, C, C++, C#, Rust, Zig, SQL — tree-sitter compiled to WASM,
> zero setup) plus **drop-in grammar packs** for any other language
> (kotlin/swift/dart ship in `grammars/`), markdown docs, the universal
> adapter door, `query`/`path`/`explain`/`affected`/`hubs`, **symbol cards**
> (`card X`: signature + doc + callers/callees, no file read), the
> **deterministic wiki** (regenerated on every add), the save-result/reflect
> learning loop, merge/export (json/dot/graphml/csv/obsidian), the live
> dashboard, remote init/push/pull, and **multi-module monorepo support**
> (`scan` / `init --scan` / parallel fan-out `add` / navigator + federated
> queries — see below). Exact call edges (x/tools + LSP) are
> next — see `openspec/` for the plan.

**Site, demos, benchmarks:** https://muthuishere.github.io/ctx-optimize-site/
— landing page, unedited demos, and the full proof write-up. Everything below
is reproducible; see [Proof](#proof--reproducible-not-our-word).

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

## Multi-module — monorepos get one graph per module, plus a navigator

One giant graph for a 300-module monorepo helps nobody: people work in one
module at a time, and an agent that loads the whole repo's graph pays for
299 modules it isn't asking about. ctx-optimize builds **one store per
module** and a small **navigator** that routes questions instead:

```sh
# find every project in the tree — read-only, prints the exact config it would write
ctx-optimize scan                # markers: go.mod/go.work, package.json, gradle,
                                 # maven, Cargo.toml, pyproject… (--depth N, default 5)

# write ALL found modules into the committed config — generated once, then the
# list is yours: edit, add, prune (.ctxoptimize/config.json modules[])
ctx-optimize init --scan --yes

# gather: one worker per module, in parallel; stores mirror the repo tree
ctx-optimize add .               # → ~/ctxoptimize/<repo>/<module-path>/, each with
                                 #   its own graph + wiki  [--jobs N]
```

Measured on `apache/beam`: **310 modules discovered at depth 8, all gathered
in 14.5s at ~9× CPU, zero failures** — including maven modules nested inside
other modules' resource trees.

The root store holds a **navigator**, not a merged giant graph:
`modules.json` + `navigator.md` — every module's path, node/edge counts, top
hub symbols, and README one-liner — plus a unified wiki front page linking
into each module's own wiki. Query scope then follows your cwd:

```sh
cd sdks/java/transform-service
ctx-optimize query "expansion service"  # answers from THIS module's graph, labeled;
                                        # zero hits auto-escalate repo-wide (--root forces)
cd -                                    # back at the repo root:
ctx-optimize query "kafka read"         # navigator ranks modules, federates across the
                                        # best matches  [--modules all|a,b]
ctx-optimize card SomeSymbol            # not in your module? answered from the owning
                                        # module, labeled "[not in X — found in Y]"
```

`merge <mod>... --into <name>` stays opt-in for when you actually want one
combined graph. (graphify's monorepo story is manual per-directory builds —
no discovery, no parallel gather, no navigator.)

## Proof — reproducible, not our word

Two kinds of evidence, both runnable.

**Speed vs graphify** (raw data in [`benchmarks/`](benchmarks/)): a 12k-file
corpus gathered in **0.67s vs 8.88s**, queries **~4× faster**, a smaller
store. Methodology on the site.

**What an agent actually saves.** A headless harness lets the *same* model
answer a set of questions **three ways** over OpenRouter — plain shell,
ctx-optimize, and graphify — and reports the provider's own token/cost
accounting (`usage.include=true`), not our estimate. Last public CI run on
`gorilla/mux` (a small, well-named repo — plain grep's *best* case, i.e. the
hardest terrain for a graph to win on):

| comparison | result |
|---|---|
| ctx-optimize **vs plain shell** | **−31% cost · −64% tool calls · −36% tokens** |
| ctx-optimize **vs graphify** | **~half the tokens & tool calls** |
| graphify **vs plain shell** | **+22% tokens** — its `query` returns a raw node dump that costs *more* than grep |

ctx-optimize answers most questions in a single `query`/`card` call; both arms
answered correctly with `file:line` citations (a cheaper wrong answer is a
loss, not a saving).

**Run it yourself — no source needed**, it uses the published CLI:

```sh
npm i -g @muthuishere/ctx-optimize      # the store CLI
pipx install graphifyy                  # the competitor (arm c; optional)
export OPENROUTER_API_KEY=sk-or-...      # read from env only, never logged
bash proof/agent/run-bench.sh           # defaults: gorilla/mux, openai/gpt-4o-mini
```

Or fork and click **Run workflow** — [`.github/workflows/benchmark.yml`](.github/workflows/benchmark.yml)
runs it headless on a clean runner and publishes the table to the job summary.
Harness + full write-up: https://muthuishere.github.io/ctx-optimize-site/proof/agent/

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

## Grammar packs — add any language without recompiling

A language is just a grammar + a node-type mapping. The 12 embedded ones
cover the mainstream; anything else is a **pack**: `<name>.wasm` +
`<name>.json` dropped into `~/ctxoptimize/grammars/` (machine-wide) or
`.ctxoptimize/grammars/` (travels with the repo). Next `add` picks it up;
pack extensions override embedded ones. kotlin, swift and dart ship as packs
in `grammars/` — copy the pair in to enable.

Build your own from ANY tree-sitter grammar with one command — no toolchain
to install (zig auto-downloads once, sha256-verified; grammar fetched as a
tarball, no git):

```sh
ctx-optimize languages add kotlin        # known names resolve to the right repo/branch/exts
ctx-optimize languages add https://github.com/tree-sitter-grammars/tree-sitter-lua
# → ~/ctxoptimize/grammars/<name>.wasm + <name>.json (mapping auto-suggested
#   from the grammar's node-types.json — review it, then `add` just works)
ctx-optimize languages list              # embedded + packs + addable names
ctx-optimize languages remove <name>
```

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
eliminates). Extensibility is a verified differentiator, not a slogan: a
source audit of graphify (2026-07-11) found its languages, data-source lanes
and exporters are all fork-required static registries (only its remote hooks
are user-pluggable); here languages are drop-in packs, adapters are dropped
scripts, and the batch door takes any producer. Vision: `docs/VISION.md`.
Standing critique: `docs/CRITIQUE.md`.

## Lineage

With all due respect to graphify — a project we learned a great deal from —
there is a direct line between it and this tool: graphify's central graph
store and its pluggable remote push/pull hooks (the one part of graphify an
end user can extend without forking) were contributed upstream by this
project's author (graphify #1751 / #1752; git-verifiable). ctx-optimize is
that same idea carried through the whole product: the store, the languages,
the adapters, and the sync are all open seams by design — nothing here
requires a fork to extend.

## License

MIT © 2026 Muthukumaran Navaneethakrishnan

---

Made by [deemwar](https://deemwar.com) — more dev tools and products at [deemwar.com](https://deemwar.com).
