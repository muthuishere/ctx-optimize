# CLI reference — every verb: what, when, why

The mental model: **gather once, refresh cheaply, answer from the store.**
Verbs split into five groups — setup, gather, ask, share, maintain. Pick by
intent, not by habit; `query` is not the answer to everything.

Global flags on most verbs: `--path DIR` (which repo/module, default cwd) ·
`--store DIR` (store root, default `$CTX_OPTIMIZE_STORE` or `~/ctxoptimize`)
· `--json` (machine-readable output).

---

## Setup

### `up` — THE command

```sh
ctx-optimize up
```

**When**: always. Bare repo, fresh clone, teammate machine, CI, stale store —
run it whenever, it's idempotent.
**Why**: it looks at the state and does the right thing: no config →
bootstraps (monorepos via scan) and gathers · committed config with a
`remote.pull` and no local store → pulls the team's prebuilt graph (falls
back to gathering, loudly) · declared module stores missing → gathers exactly
those · stale vs git HEAD → fast re-gather · fresh → no-op. Recorded
[native sources](sources.md) re-capture after the gather (24h TTL;
`--sources=always|never`, `--strict`, `--prune-sources`).

### `init` — author-side control

```sh
ctx-optimize init [--instructions CLAUDE|AGENTS|ALL|NONE] [--scan [--yes]]
```

**When**: you want control over what `up`'s bootstrap would decide — curate
the module list, pick which agent files get the pointer block, scaffold
without gathering.
**Why**: writes the committable `.ctxoptimize/` (config.json, instructions.md,
adapter + transport samples) and the agent pointer blocks. `--scan` is the
monorepo lane — see [monorepos](monorepos.md). Re-init never rewrites
identical content.

### `scan` — read-only preview

**When**: before `init --scan`, to see exactly which modules would be
declared. **Why**: prints every project found and the exact config.json
`init --scan` would write. Changes nothing.

### `config` — settings, git-style two levels

```sh
ctx-optimize config instructions AGENTS            # machine-global
ctx-optimize config instructions AGENTS --project  # this repo, committable
```

**When**: you want different agent files targeted, or per-repo pinning.
Keys: `instructions`, `skills`, `hooks` (each `CLAUDE|AGENTS|ALL|NONE`).

---

## Gather

### `add .` — full gather

**When**: first build, and whenever you want adapter scripts included.
**Why**: code (tree-sitter, 12 embedded languages + packs), markdown docs,
framework routes, build-tool manifests, k8s topology, git co-change — plus
every adapter script in `.ctxoptimize/adapters/`. Re-gather prunes stale
nodes (producer-scoped truth). A >50% shrink of one producer is refused
unless `--force` — that guard protects module stores; the monorepo root
residual is exempt (its scope legitimately follows the module list). At a
multi-module root it fans out one worker per module (`--jobs N`) and
refreshes the navigator.

### `sync` — fast lane

**When**: "code changed, refresh the store" in your inner loop.
**Why**: `add .` minus adapter scripts — safe because Replace is
producer-scoped, so adapter nodes stay put.

### `add <ENV_NAME>` — native source

**When**: getting a database / bucket / queue / external API into the store.
**Why**: the env var's value is a URL; the scheme picks a wire-native
connector. See [sources](sources.md).

### `add --json -` — the universal door

**When**: anything else that can print a batch. **Why**: strictly validated
upsert; the whole [adapter contract](adapters.md) in one flag.

### `adapters run [name]` / `adapters list` / `adapters help <scheme>`

**When**: run the slow adapter scripts on demand (all, or one by name); see
what's registered; get the paste-ready setup card for a source scheme.

### `capture <ENV_NAME>` — debug primitive

**When**: a source misbehaves, or an adapter script needs to compose.
**Why**: one connector dial → Batch JSON on stdout, no store write.

---

## Ask — pick by intent

| Intent | Verb | Why this one |
|---|---|---|
| **Find** — you have words, want locations | `query "<2-4 terms>"` | ranked, cited, signatures; complete hits under a token budget |
| **Inspect** a known symbol | `card X` | signature + doc + callers + callees, no file read |
| **About to EDIT** | `change-plan X` | ONE call: signature + callers + blast radius + which tests to run + co-change history; replaces a query/card/affected/grep chain |
| **Blast radius** only | `affected X [--depth N]` | reverse impact: what breaks if X changes |
| **Connection** | `path "A" "B"` | shortest path between two nodes |
| **Orient** in a new repo | `hubs --top 10` | most-connected nodes; also read the generated `wiki/` |
| **Explain** a node | `explain X` | plain-language node + neighborhood |
| **Check a citation** | `verify "file.go:L10-L20"` | node exists, file exists, range in bounds, drift vs gather-time HEAD; exit 0 only when ALL claims hold |

Scope follows your cwd: inside a module dir you get that module (zero hits
escalate repo-wide); at a monorepo root, queries federate via the navigator
(`--modules all|a,b`, `--root`). Name resolution is honest: fuzzy matches
announce themselves, fuzzy ties refuse with candidates (`--fuzzy` overrides).

### `status` / `fresh`

**When**: "can I trust this store right now?" `status` prints store facts +
freshness vs git HEAD; `fresh` is the scriptable gate (exit 0 fresh / 1
stale / 2 unknown) — wire it into hooks/CI before trusting answers.

---

## Share

### `remote push` / `remote pull`

**When**: team sharing — one person gathers, everyone pulls.
**Why**: the binary ships NO transport; these run the commands YOU declare in
committed config. See [remote & GitHub](remote-github.md).

### `merge` / `export`

**When**: `merge api worker --into everything` builds one combined view
(derived — re-derive after pull, never sync it). `export --format
json|dot|graphml|csv|obsidian` dumps the graph for OTHER tools — that's not
team sharing.

---

## Maintain

| Verb | When / why |
|---|---|
| `serve` (alias `dashboard`) | visual store management on 127.0.0.1:4747 — repos, onboarding, graph viewer, query, settings; mutations stay loopback-only and audited |
| `log` | print the mutation audit trail (`audit.ndjson`: ts, actor, action, hashes) |
| `wiki` | regenerate the deterministic markdown wiki (every `add` already does) |
| `languages add <name\|url>` | any tree-sitter grammar → drop-in pack, no toolchain to install (zig auto-downloaded once, sha256-verified) |
| `routes add` / `manifests add` | teach it your framework's routes / your build tool's manifests via JSON packs |
| `save-result` / `reflect` | record how answers worked out; aggregate into `reflections/LESSONS.md` |
| `install` | skills + hooks for every agent CLI detected — see [agents](agents.md) |
| `update` | update everything: binary (sha256-verified; dev builds left alone), then skills/hooks/global rule. User-invoked ONLY — never a background check. `--check` reports without touching |
| `uninstall` | remove what install wrote; stores + committed repo pointers stay |

---

## The design contract (why you can trust the above)

The binary is deterministic: **no LLM calls, no embeddings, no database, no
credentials at rest**. Network happens only when you ask: your remote
scripts, `update`, `grammar build`'s one-time zig download, and source
capture at your explicit `add`. The only intelligence in the system is the
agent (or human) running the verbs.
