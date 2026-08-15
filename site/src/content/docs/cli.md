---
title: CLI reference
description: "Every verb, every flag: build and refresh, ask, list and filter, manage and share."
---

ctx-optimize gathers your repo — code, docs, manifests, git history, and optionally your database, bucket, queue or API — into a deterministic graph on disk, then answers questions about it with a resolved symbol, its signature and its `file:line`. One static Go binary: no database, no server, no LLM, no embeddings, no MCP.

It is configured in exactly three places — the **committed repo config** (`.ctxoptimize/config.json`), a **machine-global config** (`~/ctxoptimize/config.json`), and **one environment variable**. Everything else is a command. Use the sidebar; everything is on these five pages.

## npm, Go, or a plain binary.

```text
# npm — thin launcher + prebuilt platform binary (no postinstall, no download step)
npm install -g @muthuishere/ctx-optimize

# Go
go install github.com/muthuishere/ctx-optimize/cmd/ctx-optimize@latest

# plain binaries (macOS / Linux / Windows) — GitHub Releases
# https://github.com/muthuishere/ctx-optimize/releases

# then give your agents the skill + prompt hook (Claude Code, Codex, Copilot, Devin)
ctx-optimize install --skills

# later — one command updates the binary AND every installed surface
ctx-optimize update            # npm installs update via npm; standalone binaries from
                               # GitHub Releases, sha256-verified against checksums.txt,
                               # swapped atomically — then skills + hooks refresh
ctx-optimize update --check    # report only, touch nothing
```

`install` detects which agent CLIs are on the machine and reports per platform. Narrow with `--claude --codex --copilot --devin`, or with `--skills` / `--hooks` to install only one mechanism. Remove everything with plain `uninstall` — no flags needed; stores and committed repo pointers stay. `update`'s network call happens only when **you** run it — the binary never checks for updates in the background.

## One verb. Bare repo, monorepo, fresh clone, CI — `up` decides.

```text
# the whole getting-started story — run it whenever, it's idempotent
ctx-optimize up            # no config → bootstraps it (monorepos via scan; curate
                           #   .ctxoptimize/config.json after) and gathers
                           # config declares a remote.pull + no local store → pulls the
                           #   team's prebuilt graph (falls back to gathering, loudly)
                           # no remote → gathers · stale vs git HEAD → fast re-gather
                           # fresh → no-op

# author-side, when you want control instead of up's defaults
ctx-optimize init          # scaffolds .ctxoptimize/ (commit it): config, adapter + transport
                           #   samples, remote.example.md, agent pointer block
ctx-optimize scan                  # monorepo, READ-ONLY: lists every project + the config it would write
ctx-optimize init --scan --yes     # writes the FULL module list to config.json — yours to edit after
ctx-optimize add .                 # gather explicitly (parallel fan-out per module + a navigator)

# then ask
ctx-optimize query "where is auth"
ctx-optimize card HandleCheckout   # signature + doc + callers + callees, no file read
ctx-optimize change-plan PaymentService   # about to EDIT: callers + blast radius + which tests to run, one call
ctx-optimize affected PaymentService --depth 2
```

## How your agent learns to use the store — a skill, not an MCP server.

There is no MCP server, by choice. `install` writes an **agent skill** (plus an optional prompt hook and a committed pointer block) that teaches the agent *when* and *how* to reach for the store — the query craft, the verify discipline, the verb-by-intent ladder — instead of handing it a flat tool list.

```text
ctx-optimize install --skills          # one-time per machine: detect every agent CLI, write the skill
ctx-optimize install                   # skills + prompt hooks for every platform detected
ctx-optimize install --claude --codex  # narrow to specific platforms
ctx-optimize uninstall                 # remove everything install wrote (stores + repo config stay)
```

| what lands | where, and what reads it |
|---|---|
| the skill | `~/.claude/skills/` (Claude Code, Copilot) and/or `~/.agents/skills/` (the cross-CLI `SKILL.md` standard). Controlled by the `skills` setting. |
| the prompt hook | `~/.claude/settings.json` (Claude) and the AGENTS-family hook files (`~/.codex/hooks.json`, copilot). Controlled by the `hooks` setting. **Devin needs no hook file** — it reads the Claude hook and `AGENTS.md` natively. |
| the repo pointer block | Written by `init` into `CLAUDE.md` / `AGENTS.md`, marker-fenced so your surrounding text is never touched and a re-init never duplicates it. Controlled by the `instructions` setting. |
| `.ctxoptimize/instructions.md` | The committed **usage card** — the deep doc the pointer block references. Version-stamped managed block, refreshed upgrade-only by `init`/`up`; anything your team writes outside the markers is left alone. |

The pointer block is **gated**: it opens by telling the agent to check that `ctx-optimize` is actually installed, and to read the code normally if it isn't. A teammate who never installs the binary is never worse off. All four surfaces refresh together on `ctx-optimize update`.

## Not "search the codebase" — say what you're about to do.

Every verb answers a different question, and picking by intent is most of the value. This is the table to read first; the full flag-by-flag reference is [below](#cli).

| intent | verb | why this one |
|---|---|---|
| Find — you have words, want locations | `query "<2-4 terms>"` | ranked, cited hits with signatures, complete under a token budget |
| Inspect a known symbol | `card X` | signature + doc + callers + callees, without opening the file |
| About to EDIT | `change-plan X` | ONE call: signature + callers + blast radius + which tests to run + co-change history |
| Blast radius only | `affected X` | reverse impact — what breaks if X changes |
| Connection between two things | `path "A" "B"` | shortest chain of real edges |
| Orient in a new repo | `hubs --top 10` · `report` | most-connected nodes; `report` adds subsystems, seams, and what could NOT be resolved |
| Explain a node | `explain X` | plain-language node + neighborhood |
| Check a citation before acting | `verify "file.go:L10-L20"` | exit 0 only when every claim holds |
| List / filter without jq | `nodes` · `edges` · `deps` | native table or JSON output over the graph |
| Can I trust the store right now | `fresh` · `status` | freshness against git HEAD, scriptable exit codes |

:::note
**These verbs answer with facts only — and say when that isn't everything.** Call sites the extractor could not attribute are held back as an `AMBIGUOUS` shortlist rather than guessed at, and `card`/`explain`/`affected`/`path`/`hubs`/`change-plan` exclude them by default. Two cases get held back: a callee **name defined more than once** in the repo, and a **method reached through a receiver whose type was never established** (the store holds only *your* declarations, so it cannot tell `err.Error()` from a call to your own `Error`). So **a method's blast radius is a floor, not the full set** — `card` prints `unattributed callers: N` with the grep that settles it. Pass `--include-ambiguous` to walk the shortlist anyway: widened rows are marked (`?` on `affected`, a `MAYBE` heading elsewhere) and are candidates to verify, never callers. This is a [measured defect](/ctx-optimize/#quality), disclosed, not hidden.
:::

### Settling an abstention for good — `.ctxoptimize/resolutions.json`

`--include-ambiguous` lets you *look* at a shortlist. This lets the repo **settle** one, so the same question isn't re-derived by every agent forever:

```text
{ "external_methods": ["Error", "String", "Close"] }
```

Bare **method** names whose receivers are never types you own. Listing the name retires that shortlist — on this repo one declared line retired 98 of them. It is deliberately the one key that **cannot make the graph wrong**: it is consulted only on the abstention path, never deletes a resolved edge (`MyErr.Error()`, which names its own receiver, still resolves), and applies only to receiver-qualified calls. Malformed is a **hard error, never a warning** — bad JSON, an unknown key, a qualified name, an empty entry — because a silently ignored declaration is the worst outcome. A declared name matching no call site is reported on every gather, so the file cannot rot in silence. `init` scaffolds an inert `resolutions.json.sample`.

## The whole verb set.

Global flags on every command: `--path DIR` (which module/repo, default cwd) and `--store DIR` (store root, default `$CTX_OPTIMIZE_STORE` or `~/ctxoptimize`). Most read verbs take `--json`.

### Build & refresh

| command | what it does |
|---|---|
| init | The author-control door: scaffold `.ctxoptimize/` (config, adapter template, transport samples `push.js.sample`/`pull.js.sample`, `remote.example.md`), prepare the store, write the agent pointer block into the repo's instruction files (see [global config](#global-config)). Re-running never clobbers your config or duplicates the block; on a pull-declaring clone it redirects to `up`. Don't need control? `up` bootstraps for you. |
| init --scan [--yes] [--depth N] [--modules "globs"] | Multi-module generator: scan, confirm, write the full found list into `modules[]`. Generated once — the list is yours to edit after. |
| scan [--depth N] [--json] | Read-only module discovery. Prints every project found and the exact config `init --scan` would write. Changes nothing. |
| up | **The front door.** Makes the store exist and be current, whatever the state needs: no config → bootstraps it (monorepos via scan; curate after) and gathers; empty store + declared `remote.pull` → pull (a failed pull gathers locally and says so); no remote → gather; stale vs git HEAD → fast re-gather (adapters skipped); fresh → no-op. Idempotent. |
| add [<path>] [--force] [--jobs N] [--no-adapters] | Gather code + markdown + property/manifest files + every adapter (`--no-adapters` skips the scripts). Incremental: re-add prunes stale nodes; a >50% shrink is refused unless `--force` (a gutted gather is more likely a broken run than a huge refactor). At a multi-module root it fans out one worker per module and refreshes the navigator. Honors `.gitignore` via git itself. |
| add --json -\|FILE | The universal door: upsert a validated batch of nodes+edges from any adapter/tool. Fail-closed — an invalid batch is rejected whole. |
| sync | The fast lane: re-gather the repo you're in (code, docs, manifests, git; prunes deleted, refreshes wiki + navigator) but **skip adapter scripts**. Safe — replacement is producer-scoped, adapter nodes stay put; the report says how many were skipped. |
| adapters list \| run [name] | The slow lane, on demand: list the dropped adapter scripts, or re-run all (or one by name) when the external system changed — schema migrated, topics moved. Running one adapter never disturbs the code graph. |
| add --rebuild | The guaranteed resync: drop the store(s) first, gather into an empty one. Needed because replacement is producer-**scoped**, so a *retired* producer (a deleted adapter, a removed grammar pack) is never replaced and its nodes survive every incremental gather. A normal `add` now reports those; `--rebuild` is the certain path. Nested module stores are kept — each module is rebuilt by its own task. Audited. |
| add <ENV_NAME> | Native source: the env var's **value** is a URL whose scheme picks the connector. Resolves env → repo-root `.env` → `~/.config/ctx-optimize/.env`, dials, captures the logical shape, merges, and records the **name** in config. Names only on argv — never a raw URL. See [native sources](#sources). |
| capture <ENV_NAME> | One connector → Batch JSON on stdout, **no store write**. The debug/composition primitive — sanity-check a source before it is part of the graph. Adapter scripts call it back with their own env. |
| wiki | Regenerate the markdown wiki in the store's `wiki/` (deterministic, from nodes+edges only). Every `add` already does this unless the repo sets `"wiki": false`; this verb always builds a **complete** one, so "off" never means "unavailable". |

### Ask

| command | what it does |
|---|---|
| query\|ask "terms" [--budget N] [--modules all\|a,b] [--root] | Ranked, cited hits (id, kind, file:line, signature, neighbors). Scope follows cwd: in a module dir it answers from that module and escalates repo-wide on zero hits; at the root it federates across all modules. `--budget` caps output tokens (default 2000). |
| card "X" | Symbol card: signature, doc, location, callers, callees — cite without opening the file. Accepts id, exact label, or fuzzy name; strips invented qualifiers (`ns::Class::Method` → `Method`); a total miss suggests the nearest labels. |
| change-plan "X" [--json] | The composed "I'm about to change X" answer, one bounded call: signature + callers + blast radius + **which tests to run** + co-change history, with a confidence footer. One call in place of the query+card+affected chain. Alias: `plan`. |
| explain "X" | Plain-language node + neighborhood. |
| affected "X" [--depth N] [--relation R] | Reverse impact: what breaks if X changes. |
| path "A" "B" | Shortest path between two nodes. |
| hubs [--top N] | Most-connected nodes ("god nodes") — where to start reading. |
| report [--json] | ONE artifact for "explain this repo": subsystems, hubs, the **seams between** subsystems, and — uniquely — what the graph could **not** resolve, with the grep that settles each one. Facts only: structure never counts an `AMBIGUOUS` edge. |
| verify "<claim>" … | Citation check before a human acts on one: node exists (exact id or label, **never fuzzy**), file exists, line range in bounds, drift vs gather-time git HEAD. Claims are `node-id`, an exact label, or `file:L10-L20`. **Exit 0 only when every claim holds.** |
| status [--json] | Store facts + freshness vs git HEAD + a local tally of answers served. |
| fresh [--json] | One-line verdict and a scriptable exit code — the agent/hook gate before trusting an answer. **0** fresh · **1** stale (re-gather) · **2** unknown (no git provenance — nothing is wrong, freshness just can't be determined) · **3** **partial** (the last gather had producer lanes fail, so the store is incomplete). `partial` is its own code, not a reuse of stale, because the responses differ: stale means "old but complete", partial means "a producer is missing". `status` and `fresh --json` name the failed lanes; `up` retries a partial store in full, adapters included. |
| serve\|dashboard [--port 4747] [--host H] | Local dashboard on 127.0.0.1 — repos, onboarding, graph viewer, query, settings, change log. Zero external requests; mutations stay loopback-only even with `--host` widened, and every one is audited. |

### List & filter — no jq

Three verbs read the graph as data. Table output by default, `--json`/`--ndjson` for scripts; at a monorepo root they federate across every module.

| command | what it does |
|---|---|
| nodes [--kind K] [--file-type FT] [--id-prefix P] [--label S] [--scope S] [--where k=v,k~v] [--select f1,f2] | List/filter graph **nodes** natively. e.g. `nodes --kind service --where namespace=prod`. |
| edges [--relation R] [--confidence C] [--from ID] [--to ID] [--id-prefix P] [--where k=v] [--select f1,f2] | List/filter graph **edges**. e.g. `edges --relation resolves_to`, or `edges --relation calls --confidence AMBIGUOUS --to <id>` to see the shortlist a traversal verb held back. |
| deps [--scope runtime\|dev\|peer\|…] [--importers] | Dependencies with their scope; `--importers` adds the files that import each — one command instead of a jq join. |

### Manage & share

| command | what it does |
|---|---|
| config [<key> [<value>]] [--project] | Get/set settings, git-style two levels: machine-global by default, `--project` writes the committable repo config. Project overrides global — see [settings](#global-config). |
| remote push\|pull | Run the push/pull **commands you declared** in `.ctxoptimize/config.json` — the binary ships no transport of its own. Your command gets `CTX_STORE_DIR` / `CTX_STORE_KEY` / `CTX_SCOPE_PREFIX` / `CTX_DIRECTION` in env; a non-zero exit fails the verb. No URL argument — the config file is the single source of truth. Scope follows cwd. (`remote init` was removed in v0.4.) |
| merge <module>... --into N | Combine module stores into one merged view. Always opt-in, never automatic. |
| export [--format json\|dot\|graphml\|csv\|obsidian\|all] [--ndjson] [--kind K] [--relation R] [--where k=v] [--out F\|DIR] | Dump the graph for **other tools** — that is not team sharing. Filter flags narrow both streams (bare `export` is unchanged). `csv` with `--out DIR` writes `nodes.csv` + `edges.csv`; `obsidian` and `all` require `--out DIR`. |
| store delete [--yes] | Delete **this repo's** stores — the root store and every module store at any depth, always the whole repo whichever directory you run it from; a sibling repo is never in scope. Prints the full blast radius, then **asks** `[y/N]`; off a terminal (pipe, CI) nothing is asked and nothing is deleted. `.ctxoptimize/` is never touched — it is committed config, not a cache. Audited. |
| log [--json] | Print the mutation audit trail (`<store-root>/audit.ndjson`): timestamp, actor (`cli` or `dashboard`), action, target, before/after sha256. Append-only and sorted, so it diffs in git. |
| languages add\|list\|remove | Grammar packs — see [languages](#languages). |
| routes add\|list\|remove · manifests add\|list\|remove | Route and manifest packs — see [packs](#packs). |
| save-result / reflect | The learning loop — see [below](#learning). |
| version | Print the version. |
| install / uninstall | Agent skill + prompt hooks per platform. Plain `uninstall` removes everything `install` wrote; stores and committed repo pointers stay. |
| update [--check] | Self-update the binary (npm via npm; standalone via GitHub Releases, sha256-verified) then refresh skills + hooks + the global rule. `--check` reports only. User-invoked network only. |

## `.ctxoptimize/config.json` — the only thing we put in your repo.

Scaffolded by `init` — alongside an inert `example.js.sample` adapter, the git-lane transport samples `push.js.sample` / `pull.js.sample`, and `remote.example.md` — owned by you, committed so the whole team's agents inherit it. Every field:

```text
{
  "name": "my-service",                // store key override (default: repo basename)
  "remote": {                           // YOUR transport — any shell line (js, py, sh, inline)
    "push": "node .ctxoptimize/push.js", // run by `remote push`; gets CTX_STORE_DIR,
    "pull": "node .ctxoptimize/pull.js"  // CTX_STORE_KEY, CTX_SCOPE_PREFIX, CTX_DIRECTION in env
  },
  "adapters": [                         // optional explicit list; scripts dropped in
    {"name": "kafka", "run": "adapters/kafka.js"}   // adapters/ are auto-discovered anyway
  ],
  "modules": [                          // PRESENT ⇒ this is a multi-module ROOT
    {"path": "services/api"},           // single-path: one dir, mirrored store
    {"path": "services/worker", "name": "worker"},
    {"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]}  // multi-path: scattered folders → ONE store
  ],
  "scan": {                             // tunes scan / init --scan
    "depth": 5,                         // max depth below root (default 5)
    "markers": ["BUILD.bazel"],         // extra marker filenames
    "include": ["tools/gen"],           // globs force-added as modules
    "exclude": ["experiments/*"]        // globs pruned from the walk
  }
}
```

| field | meaning |
|---|---|
| name | Overrides the store folder under `~/ctxoptimize/`. Use when two repos share a basename or you want a custom key. |
| remote | The push/pull **commands** — plain shell lines the binary runs as-is (cwd = repo root, one env contract: `CTX_STORE_DIR`, `CTX_STORE_KEY`, `CTX_SCOPE_PREFIX`, `CTX_DIRECTION`). The binary ships no transport of its own. A legacy v0.3 URL-shaped value still loads but is inert — `push`/`pull` print the migration pointer. |
| adapters | Optional explicit adapter list. Any `.js`/`.mjs`/`.py`/`.sh` dropped into `.ctxoptimize/adapters/` is discovered automatically on `add` and must print one batch JSON to stdout. `init` seeds an inert `example.js.sample`. |
| modules | The generated, owned module list of a monorepo root. Written by `init --scan`, hand-editable, globs allowed. Present ⇒ `add` fans out and queries resolve scope against it. Two shapes: `{"path": "…"}` for a single directory, or **`{"name": "X", "paths": ["…", "…"]}`** to gather several **scattered** folders into ONE store — the .NET / Gradle case where a module's source and tests live in separate top-level dirs (`src/Billing` + `tests/Billing.Tests`). Multi-path modules extract in a single pass, so test→source calls resolve *across* the folder split. |
| module_of | (child configs only) Marks an opt-in config inside a module dir; the value is the root store key. Written automatically when you run `init` inside a declared module — never a shadow store. |
| scan | Generator tuning: `depth`, extra `markers`, `include`/`exclude` globs. Built-in markers: go.mod, go.work, package.json, pom.xml, build.gradle(.kts), settings.gradle(.kts), Cargo.toml, pyproject.toml, setup.py. |
| instructions · skills · hooks | Per-project overrides of the [machine-global settings](#global-config) — set with `config <key> <value> --project`, committed so the whole team inherits them. Empty = inherit global. |

:::note
**Secrets rule.** Credentials live in the environment and stay env-var **names** in config and scripts alike — the shell expands them only at the moment your remote command runs; values are never written, printed, or logged. Files whose names smell like secret stores (`secret`, `credential`, `password`, `.env*`) are refused by the extractors, and gathers honor your `.gitignore` via git's own semantics.
:::

## Two levels, git-style: machine-global, and `--project` overrides it.

Managed by `ctx-optimize config`. Global lives in `~/ctxoptimize/config.json` (never committed); `--project` writes the same keys into the repo's `.ctxoptimize/config.json` (committable — a team pins a repo's behavior). **Project beats global.** Keys are flat artifact nouns; values name who gets the artifact.

```text
ctx-optimize config                              # list effective values + which level set them
ctx-optimize config instructions                 # get one key (effective)
ctx-optimize config instructions CLAUDE          # set globally — scriptable (CI, npm setup, dotfiles)
ctx-optimize config instructions AGENTS --project # pin for THIS repo — commit .ctxoptimize/
```

| key | values | controls |
|---|---|---|
| instructions | `CLAUDE` · `AGENTS` · `ALL` (default) · `NONE` | Which instruction files `init` writes the pointer block into: `CLAUDE.md`, `AGENTS.md`, both, or don't touch the repo at all. The block is marker-fenced — your surrounding content is never modified, and re-init never duplicates it. |
| skills | `CLAUDE` · `AGENTS` · `ALL` (default) | Which skill directories `install --skills` writes: `~/.claude/skills` (Claude Code, Copilot), `~/.agents/skills` (the cross-CLI SKILL.md standard), or both. |
| hooks | `CLAUDE` · `AGENTS` · `ALL` (default) · `NONE` | Which platforms' prompt-hook files `install` writes: the Claude hook (`~/.claude/settings.json`), the AGENTS-family hooks (codex `~/.codex/hooks.json`, copilot), both, or none. **Devin never needs a hook file** — it reads the Claude hook *and* AGENTS.md natively, and the install report says which lane covers it. |

Typos are refused with the valid options — a bad value never silently falls back to writing files. `BOTH` is accepted as an alias for `ALL`.

## One variable in. Four handed to your remote scripts.

| variable | meaning |
|---|---|
| CTX_OPTIMIZE_STORE | Store root override (default `~/ctxoptimize`). Also how tests stay hermetic. |
| CTX_STORE_DIR · CTX_STORE_KEY · CTX_SCOPE_PREFIX · CTX_DIRECTION | Set **by the binary** in the environment of your declared `remote push/pull` command: the local store tree (pull pre-creates it), the store key, the module scope, and `push`/`pull` — so one script can serve both directions. |
| $VAR in your scripts | Your own secrets stay env-var names — the shell expands them when your command runs; nothing is ever persisted or printed. |

## One store per module, a navigator on top — never a giant merged graph.

Stores mirror your repo layout under `~/ctxoptimize/<root>/<module-path>/`. Nested modules (a Maven archetype inside a module) get their own store and are never double-extracted. A module can also be a **set of scattered folders** — `{"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]}` — gathered into one name-keyed store so source and tests that live in separate top-level dirs (the.NET / Gradle convention) share a graph and resolve calls across the split.

### Scope follows where you ask

| you ask from | what answers |
|---|---|
| a module directory | That module's store. Zero hits escalate repo-wide automatically — a miss is never a dead end. |
| the repo root | Federation across all module stores (`--modules a,b` narrows, `--root` forces the root residual only). |
| anywhere else inside the repo | The nearest config wins — resolution walks upward exactly like git finds `.git`. |

The **navigator** (`navigator.md` + `modules.json` in the root store) is the map: every module, node/edge counts, top hubs, README summary, wiki links. It refreshes on every root `add`. Code that lives outside any declared module lands in the **root residual** graph, so nothing in the repo is invisible. Stores on disk that your config no longer declares are inert — federation is config-driven — and `add` tells you they're safe to delete. `merge` stays an explicit verb.

## Push the graph, not the gather time. The remote is your script.

The binary never moves bytes to a host it chose. `remote push` / `remote pull` run the commands **you** declare in the committed config — any shell line (js, py, sh, or inline). Same trust model as adapters and npm scripts.

```text
# .ctxoptimize/config.json — committed, the whole team inherits it
{
  "remote": {
    "push": "node .ctxoptimize/push.js",
    "pull": "node .ctxoptimize/pull.js"
  }
}

# arm the scaffolded zero-dependency GIT LANE (a private repo hosts every store —
# artifacts are sorted ndjson, so git diffs and merges them cleanly)
gh repo create your-org/ctx-stores --private        # once per team
mv .ctxoptimize/push.js.sample .ctxoptimize/push.js
mv .ctxoptimize/pull.js.sample .ctxoptimize/pull.js
# set STORE_REPO_URL in both, add the "remote" block to config.json, commit
ctx-optimize remote push

# teammate, fresh clone — ONE command, done
ctx-optimize up              # runs the declared pull; falls back to a local gather, loudly

# after you gather new work
ctx-optimize add . && ctx-optimize remote push
```

Your command gets the store context in env — `CTX_STORE_DIR` (pull pre-creates it), `CTX_STORE_KEY`, `CTX_SCOPE_PREFIX` (module scope: push from a monorepo root shares the whole tree, from a module dir only that module's prefix), `CTX_DIRECTION` — and a non-zero exit fails the verb. S3/R2/MinIO is a small aws-CLI script over the same contract; GCS, Artifactory, rsync-over-ssh, anything — write the script that copies `CTX_STORE_DIR` to and from your host and declare it. Recipes live in the scaffolded `.ctxoptimize/remote.example.md`. Secrets stay env-var names, expanded by the shell at run time — never in config or scripts, never printed. Vocabulary: **sync** = keep the graph matching the code (`up`, `sync`, `add.`, `fresh`); **share** = `remote push/pull`.

:::note
**Upgrading from v0.3:** `remote init` and the built-in `file://`/`s3://` transports are gone. A legacy URL-shaped config still loads but is inert — `push`/`pull` print the migration pointer.
:::

## Your database, bucket, queue and API — in the same graph, by env-var name.

Nine wire-protocol connectors. You pass the **name** of an environment variable; the scheme in that variable's *value* picks the connector. Tables, collections, topics, buckets and endpoints land as nodes beside your code, so one question spans all of it.

```text
ctx-optimize adapters help postgres      # setup card: value format, credential/cert params, paste-ready command
ctx-optimize adapters help               # every scheme it can dial

export BILLING_DB_URL='postgres://reader:$PG_PASS@db.internal:5432/billing'
ctx-optimize add BILLING_DB_URL          # resolve → dial → capture → merge → record the NAME in config
ctx-optimize capture BILLING_DB_URL      # same dial, Batch JSON to stdout, nothing written

ctx-optimize up                          # recorded sources re-capture after the gather (24h TTL)
ctx-optimize up --sources=always|never   # force or skip re-capture
ctx-optimize up --strict                 # fail instead of skipping when a var is unset
ctx-optimize up --prune-sources          # drop producers no longer declared in config
```

| scheme | what lands in the graph |
|---|---|
| postgres · mysql · mssql | Tables, columns, foreign keys. System schemas skipped; a partitioned table collapses to one node carrying `partitions: N` rather than exploding into children. |
| mongodb | Collections and inferred document shape from a **bounded** sample. |
| redis | Key-space shape — patterns and types, not values. |
| kafka · nats | Topics/subjects and partition counts. |
| s3 | Buckets and prefixes, via stdlib SigV4 — no vendor SDK. |
| openapi (http/https or a file path) | Operations, paths and schemas from a spec. |

### The logical-shape rule

A connector captures the shape a developer actually reasons about, not a dump: system schemas are skipped, partitions collapse to their parent, samples are bounded, and **every cap it hit is reported** in the output rather than silently truncating. A real Postgres capture is milliseconds, not minutes.

### Credentials are structural, not a policy you have to remember

Argv and committed config carry env-var **names** only. A literal password in a committed entry is a **hard error at config load**, not a warning. The value is resolved at dial time through a ladder — process environment → repo-root `.env` → machine-global `~/.config/ctx-optimize/.env` — and is choked out of every output path, so it cannot appear in a log, an error message, or the store. A tracked `.env` earns a warning. A teammate *without* the credential still runs `up` cleanly: one skip line, prior nodes stay, the gather succeeds.

Freshness is per source: `<store>/sources.json` stamps each one, dialling is parallel while the merge is serial, and each source reports `captured` / `skipped` / `failed` on its own — one unreachable database never fails the gather. Producer ids are `source:<VAR_NAME>`, so re-capture replaces exactly that slice.

:::note
**The main binary carries no database drivers.** They live in a companion binary, `ctx-optimize-adapters`, shipped beside the main one in every archive and npm package. Main execs it only inside `add`/`up`/`capture`, at your command, passing **names only** on argv — the child re-resolves the env ladder itself. If the companion is missing you get a loud error and an install hint, never a silent skip.
:::

## Anything becomes nodes: databases, topics, logs, documents.

The binary never grows drivers. An adapter is any script in `.ctxoptimize/adapters/` (`.js`/`.mjs` → node, `.py` → python3, `.sh` → sh) that prints one batch to stdout. It runs on every `add`; an invalid batch is rejected whole. Adapters can be arbitrarily slow (DB dumps, doc converters), so they get their own lanes: `sync` re-gathers the repo and **skips** them, `adapters run [name]` re-runs all or one on demand, `add --no-adapters` is the fast lane spelled long.

```text
{
  "producer": "kafka",
  "nodes": [
    {"id": "kafka://orders", "label": "orders", "kind": "topic",
     "file_type": "messaging", "source": "kafka://orders", "location": "L1"}
  ],
  "edges": [
    {"source": "kafka://orders", "target": "svc://billing",
     "relation": "consumed_by", "confidence": "EXTRACTED"}
  ]
}
```

Replacement is **producer-scoped**: each adapter owns its slice of the graph; re-runs prune what it no longer emits, and other producers' nodes are untouched. `confidence` is honest provenance: `EXTRACTED` = parsed fact, `INFERRED` = name-matched. Cross-batch edges are the point — code ↔ docs ↔ schema link by node id. Read secrets from the environment inside your script; never hardcode values.

## 12 embedded, everything else is a drop-in pack.

Embedded (tree-sitter compiled to WASM, zero setup): **Go, Python, JavaScript, TypeScript/TSX, Java, C, C++, C#, Rust, Zig, SQL**. Any other language is a grammar pack — `<name>.wasm` + `<name>.json` mapping — in `~/ctxoptimize/grammars/` (machine) or `.ctxoptimize/grammars/` (repo, committable; kotlin/swift/dart ship in the repo's `grammars/`). Packs are discovered at add-time and override embedded grammars for their extensions; a malformed pack fails loudly.

```text
ctx-optimize languages list            # embedded + installed packs + names addable by name
ctx-optimize languages add kotlin      # known name — builds the pack in pure Go (zig auto-fetched once, sha256-verified)
ctx-optimize languages add https://github.com/tree-sitter/tree-sitter-ruby
ctx-optimize languages remove kotlin
```

A pack is built in **pure Go**: no toolchain to install, no shell script in your path. A Zig compiler is taken from `PATH` or auto-downloaded once — sha256-verified against ziglang.org's own index — into `~/ctxoptimize/toolchain/` and cached. The node-type mapping is drafted for you from the grammar's `node-types.json` and marked `_review` so you know what to check. That download is the second of the two user-invoked network calls the binary makes; the other is `update`.

## Teach it your framework, without forking it.

Core recognizers stay embedded; everything else is a drop-in JSON pack, discovered at add time from `.ctxoptimize/routes/` or `.ctxoptimize/manifests/` in the repo and `~/ctxoptimize/routes|manifests/` on the machine. Repo wins on collision; a malformed pack fails loudly. The file existing IS the registration — commit it and the whole team's agents inherit it.

| axis | core, embedded | what a pack adds |
|---|---|---|
| routes | FastAPI, Flask, Express, NestJS, Angular, React Router, Vue Router (from source); OpenAPI, Drupal `*.routing.yml`, Kubernetes Ingress (from YAML) | Any call-shaped convention of your own — `registerRoute(path, handler)` — declared as selectors in JSON. |
| manifests | package.json, pom.xml, `*.csproj`/`*.sln`, go.mod, build.gradle, Kubernetes | Declarative path selectors over any json/xml/yaml build file. |
| grammars | 12 embedded languages | [Grammar packs](#languages) — `<name>.wasm` + mapping. |
| adapters | — | [Any script](#adapters) that prints one validated batch. |

```text
ctx-optimize routes add myframework            # scaffold .ctxoptimize/routes/myframework.json
ctx-optimize routes add https://github.com/org/route-packs
ctx-optimize routes list                       # core channels + discovered packs
ctx-optimize routes remove myframework         # repo first, then global

ctx-optimize manifests add internal-deps       # --global writes to ~/ctxoptimize/manifests/
ctx-optimize manifests list
```

Route edges are emitted as `INFERRED` with a `synthesized_by` channel tag — honest provenance about which recognizer produced them.

## Plain files. Sorted output. Diffable forever.

```text
~/ctxoptimize/
├── config.json                # machine-global settings (instructions, skills)
├── grammars/                  # machine-wide grammar packs
└── <repo-name>/               # one store per repo ("name" in repo config overrides)
    ├── graph/nodes.ndjson     # the graph — sorted, atomic-rename writes
    ├── graph/edges.ndjson
    ├── manifest.json          # content hashes → incremental add
    ├── source.json            # git HEAD at gather time → the fresh gate
    ├── wiki/index.md          # deterministic wiki, regenerated on every add
    ├── reflections/LESSONS.md # the learning loop's output
    ├── navigator.md           # (multi-module roots) the module map
    ├── modules.json
    ├── graph/index/           # machine-local lookup index — see below; never transported
    ├── audit.ndjson           # append-only mutation trail (ctx-optimize log)
    ├── sources.json           # per-source freshness stamps (native sources, 24h TTL)
    └── services/api/          # (multi-module) one nested store per module, mirroring the repo
```

## `card` on the Linux kernel: 1.8 s → under 20 ms.

Shipped 2026-08-05. Every verb used to materialize the whole graph to answer about one symbol — reading all 2.1 GB of a kernel store costs 0.12 s, but `json.Unmarshal` of it costs 3.19 s, so **~97% of a lookup was deserialization paid before the question was even known**.

```text
<store>/graph/index/
  labels.idx            # plain sorted text — binary-searched with 8KB ReadAt windows
  ids.idx
  edges-by-source.idx
  edges-by-target.idx   # only the matching records are ever parsed
```

| fact | measured |
|---|---|
| exact `card <symbol>`, Linux kernel | 1.73–1.83 s → **0.00–0.02 s** |
| fuzzy / ambiguous names | **unchanged, ~7.4 s** — deliberately: they rank against every node, and refusing to guess is the point |
| index size | **0.43 GB, 20% of the graph** (CodeGraph's index is 54% of their DB), built in ~6 s |
| added to a gather | ~5% on the kernel, ~5% on small corpora |
| equivalence proof | **2,002 of 2,002** kernel resolutions identical through both paths |

:::note
**It is an optimization that fails safe.** The header records the source file's size and modtime; on any mismatch — absent, stale, truncated, partial — the caller falls back to the full scan. **It can make an answer fast; it cannot make one wrong.** It is machine-local (byte offsets into *this* machine's graph), excluded from the manifest, and never transported by a remote push. The differential test that gates it caught four real defects during development: JSON escapes in index keys (11,798 kernel labels, 0.414%), a case-sensitivity divergence, a self-edge double-count, and a partial index reporting itself current.
:::

Wired into `card` only so far, and only for exact-id / exact-label resolution. `change-plan`, `affected` and `path` are follow-ups — each needs its own equivalence proof before it ships.

## The store remembers how its answers worked out.

```text
# after an answer proves useful (or doesn't) — the agent records it
ctx-optimize save-result --question "where is auth" --answer "internal/auth" \
  --type query --nodes "auth.go::login" --outcome useful      # or dead_end | corrected

# at session start — aggregate with recency decay into reflections/LESSONS.md
ctx-optimize reflect
```

Deterministic, no model anywhere: the agent is the judge, the binary only tallies — preferred nodes (corroborated, recency-weighted), dead ends to avoid, verbatim corrections. `status` shows the running tally of answers served. It is a local counter, not a savings claim — we do not claim token savings anywhere ([CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md)).

## A lookup index — and a round of corrections to our own benchmarks.

Tagged and published to npm on 2026-08-05.

### The lookup index

`card` on the Linux kernel went from **1.8 s to under 20 ms**, because ~97% of a lookup was deserializing the whole graph before the question was even known. Plain sorted text files under `<store>/graph/index/`, binary-searched with 8 KB `ReadAt` windows. It fails safe — any mismatch falls back to the full scan — and it is machine-local, so it is never transported. Full detail, sizes and the equivalence proof: [the lookup index](#index).

### Benchmarks re-run, and several published numbers corrected

The 2026-07-24 harness was a scratch script that was never committed, so its numbers could not be reproduced from the repo. It now lives in the repo at `benchmarks/bench_multi.py` with pinned, shallow clones under `benchmarks/suite/`. Two corrections, both of which had flattered us or a competitor: CodeGraph's kernel query published as **536 ms → 880 ms** (the harness passed CodeGraph only the first word while every other tool answered the full phrase), and CodeGraph's flask query published as **416 ms → 102 ms** (it resolves its store from the working directory, and the harness invoked it from the wrong one). The whole 2026-07-24 head-to-head was retired rather than patched. Both corrections stay printed next to the tables on the [comparison page](/ctx-optimize/compare/).

## The remote is your script. Onboarding is one verb.

Same contract as ever — deterministic binary, no LLM, no DB — and the network surface is now spelled exactly: **only when you ask** (`update`, `grammar build`'s one-time zig fetch, and whatever your own remote scripts do).

### Scripted remotes — the binary ships no transport (breaking)

`remote push` / `remote pull` now run the commands **you** declare in the committed `.ctxoptimize/config.json` — any shell line (js, py, sh, inline). The built-in `file://` + `s3://` transports and `remote init` are **gone**; a legacy URL-shaped config still loads but is inert, and `push`/`pull` print the migration pointer. Your command receives the store context in env — `CTX_STORE_DIR`, `CTX_STORE_KEY`, `CTX_SCOPE_PREFIX`, `CTX_DIRECTION` — and a non-zero exit fails the verb. `init` scaffolds a complete zero-dependency **git lane** as inert samples (`push.js.sample` / `pull.js.sample`) plus `remote.example.md` with git / s3 / custom recipes. Details in [Remotes & sharing](#remotes).

### `up` — the one onboarding verb, bare repo included

Bare repo, fresh clone, teammate machine, CI: `ctx-optimize up` makes the store exist and be current, whatever it takes. No config at all → bootstraps it (monorepos via scan; curate `.ctxoptimize/config.json` after) and gathers; empty store + declared `remote.pull` → run it (a failed pull falls back to a local gather, loudly); no remote → gather; stale vs git HEAD → fast re-gather (adapter scripts skipped); fresh → no-op. Idempotent — run it whenever. `init` stays the author-control door (review module lists, pick pointer targets, transport samples); on a pull-declaring clone it just redirects to `up`. CI gate: `ctx-optimize up && ctx-optimize fresh`.

### Fast lane / slow lane — `sync` and `adapters run`

Adapter scripts can be arbitrarily slow (DB dumps, doc converters), so they get their own lanes. `sync` re-gathers the repo you're in — code, docs, manifests, git — and **skips** adapters (safe: replacement is producer-scoped, adapter nodes stay put). `adapters list` shows them; `adapters run [name]` re-runs all or one on demand when the external system changed. `add --no-adapters` is the fast lane spelled long.

### `update` — the whole tool, one command

Updates the binary (npm installs via npm; standalone binaries from GitHub Releases, sha256-verified against the release's `checksums.txt`, swapped atomically — dev builds left alone) and then refreshes skills, hooks and the global rule from the binary that is now current. `update --check` reports without touching anything. User-invoked network only — never a background check.

### `change-plan` — the composed "I'm about to change X" answer

One bounded call returns the signature, callers, blast radius, **which tests to run** (affected filtered to test declarations), historical co-changes, and a confidence footer separating extracted from inferred evidence. **One call in place of the query → card → affected chain** it replaces. Alias: `plan`. (We do not claim it saves you tokens — that claim was measured and killed; see [CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md) and the [graded run](/ctx-optimize/#quality), where the store arm made 15.0 tool calls per run against the shell arm's 42.7.)

## Routes, dependencies, subsystems, co-change — and a real dashboard.

Every one of these is deterministic pure-Go extraction over the ASTs and git history we already hold — no model, no network, same contract.

### v0.3.5 — scattered modules, one-step clones, a viewer that never white-screens

**Multi-path modules.** A module can be a *set* of folders, not one subtree: `{"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]}`. The.NET / Gradle case — source in `src/`, tests in `tests/` — gathers into **one** store and extracts in a single pass, so test→source calls resolve *across* the split instead of breaking at a folder boundary. **One-step clones.** A fresh clone fetches the team's prebuilt graph instead of rebuilding from source — since v0.4 the verb for this is `ctx-optimize up` (`init` on a pull-declaring clone just redirects to it). The agent-instruction block `init` writes is gated — if the binary isn't installed, agents ignore it and read code normally. **A crash-proof viewer.** One malformed graph node used to blank the whole dashboard; now a bad node is sanitized or dropped alone and every healthy node still renders, behind an error boundary — covered by unit tests.

### Framework routes — the edge grep can't produce

Routing declarations become `route` nodes linked by `handles` edges to their handlers, so `affected <handler>` surfaces the URL that binds it and `query "GET /users"` finds the endpoint. Core recognizers: **FastAPI, Flask, Express, NestJS, Angular, React Router, Vue Router** (from source) and **OpenAPI, Drupal `*.routing.yml`, Kubernetes Ingress** (from YAML). Edges are `INFERRED` with a `synthesized_by` channel tag — honest provenance. Anything call-shaped that isn't core is a **route pack** (see below).

### The manifest lane — dependencies + K8s as graph

Build manifests become a dependency graph: **package.json, pom.xml, *.csproj/*.sln (ProjectReference = the.NET module graph), go.mod, build.gradle**. A dependency is a version-free node (`dep:maven/org.apache.kafka:kafka-clients`) with the version on the edge — so the *same* library declared by a Maven module and a Gradle module federates to **one node**, and `affected dep:npm/express` lists every module that uses it. **Kubernetes** manifests become topology: resource nodes (`k8s://ns/kind/name`), Service→Deployment `selects` (label match), Ingress→Service, ConfigMap mounts, image refs. Secret resources: node only, data never read; Helm templates skipped.

### Community-detected subsystems

The wiki now opens with a **Subsystems** table — deterministic community detection clusters the graph into architecture neighborhoods ("this repo is 6 subsystems"), each with its top hubs and dominant directories. Regenerated on every add, byte-stable.

### Git-history co-change edges

From `git log` alone (zero parsing): `co_changed_with` edges between files that historically change together, with support counts. Answers what no AST can — "what usually breaks with this?" — and the strongest signals are often **code↔doc** couplings a parser could never see.

### Pack doctrine — extend routes and manifests like grammars

Core stays embedded; everything else is a drop-in JSON pack, discovered at add time from `.ctxoptimize/routes/` (or `manifests/`) in the repo and `~/ctxoptimize/routes/` on the machine — repo wins on collision, malformed packs fail loudly.

```text
# route pack: teach it your team's own registerRoute(path, handler) convention
ctx-optimize routes add myframework            # scaffold .ctxoptimize/routes/myframework.json
ctx-optimize routes add https://github.com/org/route-packs
ctx-optimize routes list                       # core channels + discovered packs

# manifest packs work the same way (declarative path selectors over json/xml/yaml)
ctx-optimize manifests add internal-deps
ctx-optimize manifests list
```

### First-class dashboard

`ctx-optimize serve` is now a full React app (embedded, zero external requests): **Repos** (freshness, counts), **Onboard** (scan → confirm → add with live progress), **Query**, **Viewer** (force-directed graph, budgeted, click-to-expand neighborhoods), **Settings** (every config key + pack across all four axes, editable), **Changes** (the audit feed). All mutations run through the CLI's own validated doors, loopback-only even if `--host` is widened, and every change is written to an append-only `audit.ndjson` — read it with `ctx-optimize log`.

## What the binary will never do.

No LLM calls. No database drivers. No embeddings. No MCP server. No network — except when **you** ask: `update` (npm / GitHub Releases, sha256-verified), `languages add`'s one-time zig fetch, and whatever your own remote scripts do. No telemetry. The host agent is the only intelligence; the binary is deterministic and its artifacts are plain, sorted, git-diffable files. Full reasoning: [VISION.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/VISION.md) and the standing counter-weight [CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md).

**[](/ctx-optimize/)[](https://github.com/muthuishere/ctx-optimize)[](https://www.npmjs.com/package/@muthuishere/ctx-optimize)[](https://github.com/muthuishere)
