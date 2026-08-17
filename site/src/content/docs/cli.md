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

Every verb below: **when** to run it, **what to type**, **what happens**. Global flags: `--path DIR` (default cwd), `--store DIR` (default `$CTX_OPTIMIZE_STORE` or `~/ctxoptimize`). Most read verbs take `--json`.

### up

**When.** First time in a repo, or you don't want to think. Fresh clone, CI, monorepo, already gathered — one verb.

**Do.** `ctx-optimize up`

**Does.** No config → writes `.ctxoptimize/` (monorepos via scan) and gathers. Empty store + `remote.pull` → pull (failed pull gathers locally). No remote → gather. Stale vs HEAD → fast re-gather, adapters skipped. Fresh → no-op. Idempotent.

### init

**When.** You want to edit the config before the first gather, or write the pointer into `AGENTS.md` / `CLAUDE.md`.

**Do.** `ctx-optimize init` · `init --scan [--yes] [--depth N] [--modules "globs"]`

**Does.** Scaffolds `.ctxoptimize/` (config, adapter sample, `push.js.sample` / `pull.js.sample`). Does not clobber an existing config. `--scan` writes `modules[]`. A pull-declaring clone redirects to `up`. If you don't need control, use `up`.

### scan

**When.** You want to see what `init --scan` would write, without writing it.

**Do.** `ctx-optimize scan [--depth N] [--json]`

**Does.** Prints every project found. Changes nothing.

### add

**When.** Gather or refresh this tree. Also the door for adapters and native sources.

**Do.**
```text
ctx-optimize add [<path>] [--force] [--jobs N] [--no-adapters]
ctx-optimize add --json -|FILE
ctx-optimize add --rebuild
ctx-optimize add BILLING_DB_URL
```

**Does.** Walks code, markdown, manifests, adapters (honors `.gitignore`). Incremental: prunes stale nodes; &gt;50% shrink refused without `--force`. `--json` upserts a validated Batch (fail-closed). `--rebuild` drops the store first (retired producers otherwise survive). `add NAME` dials the env var's URL, merges, records the **name** in config — never a raw URL on argv. Multi-module root: one worker per module.

### sync

**When.** You edited code and want the store current. Fast path.

**Do.** `ctx-optimize sync`

**Does.** Re-gathers code, docs, manifests, git. **Skips adapters.** Adapter nodes stay. Report says how many were skipped.

### adapters

**When.** The external system changed (schema, topics) or you want to see what's dropped in.

**Do.** `ctx-optimize adapters list` · `adapters run` · `adapters run postgres` · `adapters help postgres`

**Does.** `list` shows scripts. `run` re-runs all or one. Never touches the code graph. `help` prints the source setup card.

### capture

**When.** You want to see what a source would emit before it lands in the store.

**Do.** `ctx-optimize capture BILLING_DB_URL`

**Does.** One connector → Batch JSON on stdout. No write. Adapters call this back with their own env.

### wiki

**When.** You turned `"wiki": false` on gather but want the map now.

**Do.** `ctx-optimize wiki`

**Does.** Writes a complete `wiki/` from the graph. `add` already does this unless wiki is off.

### query

**When.** You have words, not a symbol name.

**Do.** `ctx-optimize query "refund tax" [--budget N] [--modules all|a,b] [--root]`

**Does.** Ranks nodes (IDF / prefix / trigram), attaches 1-hop neighbours, stops at the token budget (default 2000). Alias: `ask`. Scope follows cwd; zero hits escalate repo-wide. [How scoring works](/ctx-optimize/concepts/#why-query-is-not-grep).

### card

**When.** You have the name. You want callers without opening the file.

**Do.** `ctx-optimize card Store.Merge`

**Does.** Signature, doc, `file:line`, callers, callees. Id, exact label, or fuzzy. Strips invented qualifiers. Miss → nearest labels.

### change-plan

**When.** You are about to edit.

**Do.** `ctx-optimize change-plan Store.Merge [--json]`

**Does.** Signature + callers + blast + tests to run + co-change, with a confidence footer. Alias: `plan`.

### explain

**When.** You want a sentence, not a card.

**Do.** `ctx-optimize explain PaymentService`

**Does.** Plain-language node + neighbourhood.

### affected

**When.** Blast radius only.

**Do.** `ctx-optimize affected Store.Merge [--depth N] [--relation R]`

**Does.** Reverse impact. AMBIGUOUS callers excluded unless `--include-ambiguous`. Floor, not a census.

### path

**When.** You know two names and want the connection.

**Do.** `ctx-optimize path cmdAdd Store.Merge`

**Does.** Shortest path on real edges.

### hubs

**When.** Unfamiliar repo. Where to start.

**Do.** `ctx-optimize hubs [--top N]`

**Does.** Most-connected nodes.

### report

**When.** "Explain this repo" as one artifact.

**Do.** `ctx-optimize report [--json]`

**Does.** Subsystems, hubs, seams, and what could **not** resolve (with the grep that settles it). Never counts an AMBIGUOUS edge as structure.

### verify

**When.** A human is about to act on a citation.

**Do.** `ctx-optimize verify "pay.go:L1-L5"` · `verify Store.Merge`

**Does.** Node exists (exact id or label, never fuzzy), file exists, range in bounds, no drift vs gather HEAD. **Exit 0 only if every claim holds.**

### status

**When.** You want the store's facts.

**Do.** `ctx-optimize status [--json]`

**Does.** Counts, freshness vs HEAD, answers served. Names failed lanes if partial.

### fresh

**When.** A hook or script must decide whether to trust the store.

**Do.** `ctx-optimize fresh [--json]` · check `$?`

**Does.** **0** fresh · **1** stale · **2** unknown (no git) · **3** partial (a producer failed). `up` retries a partial store in full, adapters included.

### serve

**When.** You want the picture.

**Do.** `ctx-optimize serve [--port 4747] [--host H]`

**Does.** Dashboard on `127.0.0.1:4747`. Alias: `dashboard`. Writes stay loopback + token. [Lock](/ctx-optimize/dashboard/).

### nodes

**When.** List/filter without jq.

**Do.** `ctx-optimize nodes --kind port --where transport=network.http`

**Does.** Table of nodes. `--kind` `--file-type` `--id-prefix` `--label` `--scope` `--where k=v` `--select`. Federates at a monorepo root.

### edges

**When.** Same, for edges.

**Do.** `ctx-optimize edges --relation calls --confidence AMBIGUOUS --to <id>`

**Does.** Table of edges. `--relation` `--confidence` `--from` `--to` `--where`.

### deps

**When.** You want dependency scope, or who imports a package.

**Do.** `ctx-optimize deps --scope runtime` · `deps --importers`

**Does.** `dep:` nodes with scope. `--importers` adds the files.

A filter value that exists **nowhere** in the store is disclosed (`no node has kind "route"; kinds present: …`). Exit 0. A real value narrowed to empty is silent. `--json`: disclosure on stderr.

### config

**When.** Read or set a setting.

**Do.** `ctx-optimize config` · `config <key>` · `config <key> <value> [--project]`

**Does.** Machine-global by default. `--project` writes `.ctxoptimize/config.json`. Project overrides global. [Settings](#global-config).

### remote

**When.** Push or pull the store. The binary has no transport.

**Do.** `ctx-optimize remote push` · `remote pull`

**Does.** Runs the command in config (`remote.push` / `remote.pull`). Env: `CTX_STORE_DIR` `CTX_STORE_KEY` `CTX_SCOPE_PREFIX` `CTX_DIRECTION`. Non-zero exit fails the verb. No URL argument.

### merge

**When.** You want one combined view of named modules.

**Do.** `ctx-optimize merge api worker --into everything`

**Does.** Writes a merged store. Opt-in, never automatic.

### export

**When.** Another tool needs the graph. Not team sharing (that's `remote`).

**Do.** `ctx-optimize export [--format json|dot|graphml|csv|obsidian|all] [--out F|DIR]`

**Does.** Dumps nodes/edges. `csv` / `obsidian` / `all` need `--out DIR`.

### store

**When.** Delete this repo's stores.

**Do.** `ctx-optimize store delete [--yes]`

**Does.** Root + every module store. Prints the blast radius, then `[y/N]`. Off a TTY: no ask, no delete. `.ctxoptimize/` stays. Audited.

### log

**When.** What mutated the store.

**Do.** `ctx-optimize log [--json]`

**Does.** Prints `audit.ndjson`: ts, actor (`cli`|`dashboard`), action, target, before/after sha256.

### languages

**When.** Add a language that is not embedded.

**Do.** `ctx-optimize languages add|list|remove`

**Does.** Grammar packs. [Languages](#languages).

### routes

**When.** A framework the core recognizers don't cover.

**Do.** `ctx-optimize routes add|list|remove`

**Does.** Route packs. [Packs](#packs).

### manifests

**When.** A build tool the core recognizers don't cover.

**Do.** `ctx-optimize manifests add|list|remove`

**Does.** Manifest packs. [Packs](#packs).

### install

**When.** Teach the agent CLI.

**Do.** `ctx-optimize install [--claude|--codex|--copilot|--devin]` · `uninstall`

**Does.** Writes skill + hooks. `uninstall` removes what `install` wrote. Stores and committed pointers stay.

### update

**When.** You want the new binary.

**Do.** `ctx-optimize update` · `update --check`

**Does.** npm channel → `npm i -g`. Standalone → GitHub Release, sha256 vs checksums.txt. Then refreshes skills/hooks. `--check` reports only. No background check.

### version

**When.** Which binary.

**Do.** `ctx-optimize version`

**Does.** Prints the version.

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

### postgres
**When.** You want tables, columns, FKs in the graph.  
**Do.** `adapters help postgres` then `add $BILLING_DB_URL` (`postgres://` or `postgresql://`).  
**Does.** System schemas skipped. Partitions collapse to `partitions: N`.

### mysql
**When.** Same, MySQL.  
**Do.** `add $MYSQL_URL` (`mysql://`).  
**Does.** Tables, columns, FKs. System schemas skipped.

### mssql
**When.** Same, SQL Server.  
**Do.** `add $MSSQL_URL` (`mssql://` or `sqlserver://`).  
**Does.** Tables, columns, FKs.

### mongodb
**When.** Collections and document shape.  
**Do.** `add $MONGO_URL` (`mongodb://` or `mongodb+srv://`).  
**Does.** Bounded sample. Not a dump of every document.

### redis
**When.** Key-space shape, not values.  
**Do.** `add $REDIS_URL` (`redis://` or `rediss://`).  
**Does.** Patterns and types.

### kafka
**When.** Topics and partitions.  
**Do.** `add $KAFKA_URL` (`kafka://`).  
**Does.** Topic nodes, partition counts.

### nats
**When.** Subjects.  
**Do.** `add $NATS_URL` (`nats://`).  
**Does.** Subject nodes.

### s3
**When.** Buckets and prefixes.  
**Do.** `add $S3_URL` (`s3://`).  
**Does.** Stdlib SigV4. No SDK. Prefixes, not objects.

### openapi
**When.** An HTTP API spec.  
**Do.** `add $SPEC_URL` (`http://` / `https://`) or `add ./openapi.json`.  
**Does.** Operations, paths, schemas.

### drop-in
**When.** No connector for it (tickets, Firebase, a log).  
**Do.** Drop `.js` / `.py` / `.sh` in `.ctxoptimize/adapters/`. Print one Batch.  
**Does.** Runs on `add`. Invalid batch rejected whole. `sync` skips it. `adapters run` refreshes it.

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
