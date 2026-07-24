# ctx-optimize

[![CI](https://github.com/muthuishere/ctx-optimize/actions/workflows/ci.yml/badge.svg)](https://github.com/muthuishere/ctx-optimize/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/muthuishere/ctx-optimize.svg)](https://pkg.go.dev/github.com/muthuishere/ctx-optimize)
[![npm](https://img.shields.io/npm/v/@muthuishere/ctx-optimize?logo=npm)](https://www.npmjs.com/package/@muthuishere/ctx-optimize)
[![benchmark](https://img.shields.io/badge/benchmark-run%20it%20yourself-4ade80)](proof/agent/)
[![platforms](https://img.shields.io/badge/platforms-macOS%20%7C%20Linux%20%7C%20Windows-blue)](https://www.npmjs.com/package/@muthuishere/ctx-optimize)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Gather a codebase — and its world — into one local knowledge store an AI agent answers from. Deterministic. No LLM API. No DB. Gather once, refresh cheaply, never go everywhere every time.**

Your coding agent burns its context window on grep-and-read: to answer one
question it greps, opens files, chases callers, re-reads. ctx-optimize turns a
repo — plus, via adapters, database schemas, messaging topics, log shapes,
documents — into a queryable graph stored as plain files in a central
per-module store, and your agent (Claude Code, Codex, Devin — any skill-capable
harness) answers *from the store* in a single call. The binary is
deterministic — no LLM, no DB, and network only when you ask: `update`
(releases), `grammar build` (zig, downloaded once), and whatever your own
remote scripts do. The only intelligence in the system is the agent you
already run.

> **Status: v0.4.** On npm (`@muthuishere/ctx-optimize`) with
> prebuilt binaries for macOS / Linux / Windows; CI green; benchmarks
> reproducible (see [Proof](#proof--reproducible-not-our-word)). Working
> today: code extraction for **12 embedded languages** (Go, Python, JS,
> TS/TSX, Java, C, C++, C#, Rust, Zig, SQL — tree-sitter compiled to WASM,
> zero setup) plus **drop-in grammar packs** for any other language
> (kotlin/swift/dart ship in `grammars/`), markdown docs, the universal
> adapter door, `query`/`path`/`explain`/`affected`/`hubs`, **symbol cards**
> (`card X`: signature + doc + callers/callees, no file read),
> **`change-plan`** (one composed answer for "I'm about to change X":
> signature + callers + blast radius + which tests to run), the
> **deterministic wiki** (regenerated on every add) with a
> **community-detected "Subsystems" map**, the save-result/reflect
> learning loop, merge/export (json/dot/graphml/csv/obsidian), **scripted
> remote push/pull**, and **multi-module monorepo support** (`scan` /
> `init --scan` / parallel fan-out `add` / navigator + federated queries).
> New on main (unreleased): **native sources** — databases, buckets, queues,
> and external APIs enter the store by env-var name (`ctx-optimize add
> BILLING_DB_URL`); 9 wire-protocol connectors in a companion binary — see
> [Databases, buckets, queues, APIs](#databases-buckets-queues-apis--native-sources).
> New in v0.4 (**breaking**): **the remote is your script** — the binary
> ships no transport of its own; `remote push`/`pull` run the commands you
> declare in the committed config (`remote init` and the built-in
> `file://`/`s3://` lanes are gone — see [Sharing](#sharing--the-remote-is-your-script)),
> **`up`** (the one onboarding verb: pull, gather, or refresh — whatever the
> state needs), **`sync`** + **`adapters run`** (fast lane / slow lane around
> adapter scripts), and **`update`** (self-updates the binary and every
> installed surface — sha256-verified, user-invoked only).
> New in v0.3: **framework routes** (FastAPI/Flask/Express/NestJS/Angular/
> React Router/Vue + OpenAPI/Drupal/Ingress YAML — route nodes linked to
> their handlers, so `affected <handler>` surfaces the URL that binds it),
> the **manifest lane** (package.json/pom.xml/csproj+sln/go.mod/gradle
> dependencies + K8s topology as graph — one `dep:` node federates across
> build tools and modules), **git-history co-change edges** ("these files
> change together", from `git log` alone), a **first-class React dashboard**
> (`serve`: onboard/repos/viewer/settings/changes, all audited), and the
> **pack doctrine** — routes and manifests are extensible with drop-in JSON
> packs (`routes add` / `manifests add`, name or GitHub URL) exactly like
> grammar packs. Exact call edges (x/tools + LSP) are next — see `openspec/`.

**Docs:** [`docs/`](docs/) — [CLI reference](docs/cli.md) (every verb: when & why) · [monorepos](docs/monorepos.md) · [share the store over GitHub](docs/remote-github.md) · [native sources](docs/sources.md) · [custom adapters](docs/adapters.md) · [agent integration](docs/agents.md)

**Demos, benchmarks, proof:** [`benchmarks/`](benchmarks/) · [`proof/`](proof/) — everything reproducible from this repo.
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

Then install the agent surface — skills + hooks for every agent CLI it
detects (Claude Code, Codex, Copilot, Devin):

```sh
ctx-optimize install
```

Later, one command updates the whole tool:

```sh
ctx-optimize update           # the binary itself (npm installs via npm; standalone
                              # binaries from GitHub Releases, sha256-verified
                              # against checksums.txt, swapped atomically; dev
                              # builds left alone), then skills + hooks + the
                              # global rule from the new binary — an exact replace
ctx-optimize update --check   # report only, touch nothing
```

The network call happens only when YOU run it — the binary never checks for
updates in the background. `ctx-optimize uninstall` removes everything
`install` wrote; stores and committed repo pointers stay.

## Usage

One verb is the whole getting-started story — bare repo, fresh clone,
teammate machine, CI, stale store, doesn't matter:

```sh
ctx-optimize up
```

`up` looks at the state and does the right thing: no config → bootstraps it
(monorepos via scan; curate `.ctxoptimize/config.json` after) and gathers;
committed config with a `remote.pull` and no local store → pulls the team's
prebuilt graph (falls back to gathering, loudly); no remote → gathers;
stale vs git HEAD → fast re-gather; fresh → no-op. Idempotent — run it
whenever.

```sh
# author-side, when you want control instead of `up`'s defaults:
# scaffold .ctxoptimize/ (config, adapter + transport samples,
# remote.example.md), review monorepo module lists, pick pointer targets
ctx-optimize init

# gather / refresh explicitly (up calls these lanes for you)
ctx-optimize add .

# ask the store — complete, citable hits under a token budget
ctx-optimize query "where is the refund flow" --json

# about to change something? ONE composed call: signature + callers +
# blast radius + WHICH TESTS TO RUN + co-change history
ctx-optimize change-plan "RefundService"

# list / filter natively — NO jq, NO python, works on Windows, all modules
ctx-optimize nodes --kind service --where namespace=prod   # every prod k8s service
ctx-optimize edges --relation resolves_to                  # code→dependency links
ctx-optimize deps --scope dev --importers                  # dev deps + who imports each

# fast lane / slow lane: re-gather code without running adapter scripts;
# run adapters (DB dumps, doc converters) on demand — all, or one by name
ctx-optimize sync
ctx-optimize adapters run

# native sources: a database/bucket/queue/API by env-var name — the value
# is a URL, its scheme picks the connector; recorded, refreshed on every up
ctx-optimize add BILLING_DB_URL

# feed ANY other system through the universal adapter door (strictly validated)
./my-exotic-adapter | ctx-optimize add --json -

# combine module stores into one view; dump for other tools
ctx-optimize merge api worker billing --into everything
ctx-optimize export --format dot --out graph.dot

# see it: local dashboard (embedded single file, zero external requests)
ctx-optimize serve          # → http://127.0.0.1:4747 — graph, search, details

ctx-optimize status --json
```

- The store is **plain files** (ndjson/json/md) — diffable, portable, at
  `~/ctxoptimize/<repo-name>/`. The only thing in your repo is the
  committable `.ctxoptimize/` directory.
- **Sharing is your script.** `remote push` / `remote pull` run the commands
  declared in the committed config — the binary ships no transport of its
  own. Queries always run on the local folder. See
  [Sharing](#sharing--the-remote-is-your-script).

## Sharing — the remote is your script

The binary never moves bytes to a host it chose. `remote push` / `remote pull`
run the commands you declare in `.ctxoptimize/config.json` — any shell line
(js, py, sh, or inline):

```json
{
  "remote": {
    "push": "node .ctxoptimize/push.js",
    "pull": "node .ctxoptimize/pull.js"
  }
}
```

Your command gets the store context in env — `CTX_STORE_DIR` (the local store
tree; pull pre-creates it), `CTX_STORE_KEY`, `CTX_SCOPE_PREFIX` (module scope),
`CTX_DIRECTION` (`push`/`pull` — one script can serve both) — and a non-zero
exit fails the verb. Same trust model as adapters and npm scripts.

`init` scaffolds a complete zero-dependency **git lane** as inert samples: a
private git repo hosts every store (artifacts are sorted ndjson, so git diffs
and merges them cleanly). Arming it:

```sh
gh repo create your-org/ctx-stores --private          # once per team
mv .ctxoptimize/push.js.sample .ctxoptimize/push.js
mv .ctxoptimize/pull.js.sample .ctxoptimize/pull.js
# set STORE_REPO_URL in both, add the "remote" block to config.json, commit
ctx-optimize remote push
```

A teammate who clones the repo runs `ctx-optimize up` — done. S3/R2/MinIO is
a small aws-CLI script over the same env contract; GCS, artifactory,
rsync-over-ssh, anything: write the script that copies `CTX_STORE_DIR` to and
from your host and declare it. Recipes live in the scaffolded
`.ctxoptimize/remote.example.md`. Secrets stay env-var NAMES that the shell
expands at run time — never in config or scripts, never printed.

Upgrading from v0.3: `remote init` and the built-in `file://`/`s3://`
transports are gone. A legacy URL-shaped config still loads but is inert —
`push`/`pull` print the migration pointer.

## Databases, buckets, queues, APIs — native sources

**A source is an environment variable name. Its value is a URL. The URL
scheme picks the connector.** One command from zero to "refreshed on every
`up`":

```sh
ctx-optimize adapters help postgres    # setup card: value format, credential params, paste-ready commands
export BILLING_DB_URL='postgres://reader:$PG_PASS@db.internal:5432/billing'   # or root .env / ~/.config/ctx-optimize/.env
ctx-optimize add BILLING_DB_URL        # resolve → dial → capture → merge → recorded in config sources
```

Nine wire-protocol-native connectors — **postgres, mysql, mongodb, redis,
kafka, nats, s3** (MinIO/R2 via endpoint hosts, bare AWS via the credential
chain), **mssql**, and **openapi** (http(s) URL or a spec file path) — no
pg_dump/atlas/tbls needed on any machine. Captures are the **logical shape**
a developer reasons about: system schemas skipped, a partitioned table is one
node with `partitions: N`, bounded samples with every cap reported. Measured:
a 100-table / 3-schema postgres captures in **31 ms** including connect
(pg_dump 101 ms, atlas 248 ms, tbls 1356 ms) — and where a 100-partition
table plus 500 Timescale chunks bloat other tools to 600–716 raw tables, it
emits **101 logical tables**.

Secret hygiene is structural: argv and committed config carry env-var
**names** only (a literal password in an entry is a hard error), values
resolve process env → root `.env` → `~/.config/ctx-optimize/.env` in memory at dial
time, stored ids are sanitized, and every output is scrubbed. A teammate
without the credentials still runs `up` cleanly — that source is a one-line
skip and the nodes arrive via `remote pull`; `--strict` turns skips into CI
failures. Recorded sources refresh on `up` under a 24h TTL
(`--sources=always|never`). The drivers live in a **companion binary**,
`ctx-optimize-adapters`, shipped beside the main one in every archive and
npm package — the main binary stays driver-free and exactly as fast, and
execs the sibling only when a source dials.

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
store. Methodology beside the raw data in [`benchmarks/`](benchmarks/).

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
Harness + full write-up: [`proof/agent/`](proof/agent/)

**The model ladder** ([`benchmarks/agent-model-bench/`](benchmarks/agent-model-bench/)):
same prebuilt linux-kernel store (~274k nodes), same 8 block-layer questions,
one fresh agent session per model, answers judged blind against withheld
golden keys:

| model · harness | score /80 | avg s/question | tool calls (8 q) |
|---|---|---|---|
| Fable 5 · Claude Code | **80** | 24.6 | 23 |
| Sonnet 5 · Claude Code | **80** | 17.5 | 25 |
| Opus 4.8 · Claude Code | **79** | 19.0 | 22 |
| Haiku 4.5 · Claude Code | **72** | 13.6 | 18 |
| gpt-4o-mini · toolnexus, one-shot | **54** | 9.4 | 24 |

The store is model-portable: the cheapest Claude tier lands 90% of frontier
quality at half the wall time, and a $0.15/M-token model reaches ~70% — for
$0.015 total — when the mandatory protocol from the committed
`.ctxoptimize/instructions.md` card ("Small models & custom runtimes") is
pinned in its system prompt. Without that protocol the same small model
scored 23/80. One-shot per question beats a continuous loop: same score,
7× cheaper, no cross-question bleed.

## .ctxoptimize/ — config that travels with the repo

```
.ctxoptimize/
  config.json          name + remote commands + sources[] (+ modules[] in a monorepo)
  instructions.md      the committed usage card agents read — managed block,
                       version-stamped, refreshed by `up` (upgrade-only; your
                       edits outside the markers are never touched)
  adapters/            drop scripts here — every .js/.py/.sh runs on `add`
  push.js / pull.js    your transport scripts (init writes an inert *.sample pair)
  remote.example.md    transport recipes: git lane, s3 lane, custom
  (no secrets here)    source URLs with secrets live in the environment,
                       your root .env, or ~/.config/ctx-optimize/.env
                       (machine-global, outside the repo) — never in config
```

`config.json`:

```json
{
  "name": "my-module",
  "remote": {
    "push": "node .ctxoptimize/push.js",
    "pull": "node .ctxoptimize/pull.js"
  }
}
```

Commit the directory — it is safe by construction:

- `name` picks the store folder under `~/ctxoptimize/` (default: repo basename).
- `remote` declares the push/pull commands — plain shell lines the binary
  runs as-is (cwd = repo root). Secrets stay env-var NAMES in scripts and
  config alike; the shell expands them at run time — values are never
  written or printed.
- **Adapters are files**: dropping `kafka.js` into `.ctxoptimize/adapters/`
  is the whole registration (`.js`/`.mjs` → node, `.py` → python3, `.sh` →
  sh; other extensions inert — `init` seeds an `example.js.sample` template).
  Each script prints batch JSON to stdout; `ctx-optimize add` runs the
  built-in extractors **and** every adapter through the fail-closed door.
  Adapters can be arbitrarily slow (DB dumps, doc converters), so they get
  their own lanes: `sync` re-gathers the repo you're in and **skips** them
  (safe — replace is producer-scoped, adapter nodes stay put), `adapters
  run [name]` re-runs all or one on demand, `add --no-adapters` is the fast
  lane spelled long. One `add` refreshes the whole world; a fresh clone
  needs zero setup — `ctx-optimize up`.

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

Made by [muthuishere](https://github.com/muthuishere).
