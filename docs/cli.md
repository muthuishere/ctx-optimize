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
bootstraps (monorepos via scan) and gathers · config present but templates
missing → fills in the inert samples (never overwriting anything you wrote) · committed config with a
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

### `.ctxoptimize/config.json` — the committed knobs

| key | what |
|---|---|
| `name` | store key override (default: repo basename) |
| `remote` | `{push, pull}` — ANY shell line; the binary ships no transport |
| `sources` | native source entries, env-var **names** only |
| `modules` | generated module list of a multi-module root |
| `wiki` | `false` skips wiki generation on `add`/`up` — the graph is the query source, and `ctx-optimize wiki` still builds a complete one on demand. **Absent = true**, so adding this key never turns it off for an existing repo. Exists because the cost is unbounded and paid forever: onboarding chromium wrote 434,597 pages / 1.7 GB into one directory, and the stale-page cleanup re-reads that directory on every later gather |
| `autosync` | `off` (default) / `lazy` / `block` — code-only re-sync on a stale read |
| `instructions` / `skills` / `hooks` | which agent surfaces `install` writes |

Sibling files: `resolutions.json` (below), `adapters/`, the `push`/`pull`
transport scripts, `instructions.md`.

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
refreshes the navigator, printing live `[3/17] services/api` progress to
stderr while the detailed results stay ordered on stdout.

### `add --rebuild` — the guaranteed resync

**When**: you want the store rebuilt from nothing, or a *retired* producer's
nodes gone for certain.
**Why**: `Replace` is producer-scoped, so a producer that stops running is never
replaced and its nodes survive every incremental gather — delete an adapter
script and its nodes stay in the graph, even under `--force`. A normal `add` now
**reports** those:

```
note: 1 retired producer(s) still in the graph: mine — they no longer run, so their nodes are stale.
      prune with `ctx-optimize add . --force` (a complete run), or `store delete --yes && add .` to rebuild.
```

Reported rather than auto-pruned, because absence means either "retired" *or*
"did not run this time" (`--no-adapters`, unchanged git HEAD, a failed lane), and
deleting a lane's data because it did not run would be far worse than a stale
node. Pruning needs a run with no skips and no failures.

`--rebuild` drops the store(s) this `add` will write and gathers into an empty
one. It uses the same task plan as the gather, so it cannot drop a key the gather
will not rewrite; nested module stores are kept (each is rebuilt by its own
task); audited.

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
| **Boundaries** — what does this call out to / expose | `boundaries` | CONSUMES/PROVIDES split, grouped by transport, secrets by NAME, `file:line` each. `query` cannot reach these |
| **Check a citation** | `verify "file.go:L10-L20"` | node exists, file exists, range in bounds, drift vs gather-time HEAD; exit 0 only when ALL claims hold |

Scope follows your cwd: inside a module dir you get that module (zero hits
escalate repo-wide); at a monorepo root, queries federate via the navigator
(`--modules all|a,b`, `--root`). Name resolution is honest: fuzzy matches
announce themselves, fuzzy ties refuse with candidates (`--fuzzy` overrides).

### These verbs answer with facts only — and say when that isn't everything

Call sites the extractor could not attribute are held back as an AMBIGUOUS
shortlist rather than guessed at, and `card`/`explain`/`affected`/`path`/
`hubs`/`change-plan` exclude them. Two things get held back:

- a callee **name defined more than once** in the repo;
- a **method** reached through a receiver whose type was never established —
  the store holds only *your* declarations, so it cannot tell `err.Error()`
  from a call to your own `Error`.

So **a method's blast radius is a floor, not the full set**, and `card` prints
`unattributed callers: N` with the grep that settles it. To walk the shortlist
anyway, add **`--include-ambiguous`** to any of those verbs: widened rows are
marked (`?` on `affected`, a `MAYBE …` heading on `card`/`explain`) and are
candidates to verify, never callers. For a flat list instead:
`edges --relation calls --confidence AMBIGUOUS --to <id>`.

`report` stays facts-only by design — it has its own section for what could
not be resolved.

### Settling an abstention for good — `.ctxoptimize/resolutions.json`

`--include-ambiguous` lets you *look* at a shortlist. This lets the repo **settle**
one, so the same question is not re-derived by every agent forever:

```json
{ "external_methods": ["Error", "String", "Close"] }
```

Bare **method** names whose receivers are never types you own. The store holds only
*your* declarations, so it can never tell `err.Error()` from a call to your own
`Error` — it abstains and shows a shortlist. Listing the name retires that
shortlist. On this repo one declared line retired 98 of them.

Deliberately the one key that **cannot make the graph wrong**: it is consulted
only on the abstention path, so it never deletes a resolved edge
(`MyErr.Error()`, which names its own receiver, still resolves) and there is no
code path from a declaration to an emitted edge at all. It applies only to
receiver-qualified calls — an unqualified `Error()` is a plain function call and
may well be yours.

Malformed is a **hard error, never a warning** (bad JSON, unknown key, a
qualified name, parens, an empty entry): a silently ignored declaration is the
worst outcome, because the author believes it is in force. A declared name that
matches no call site is reported on every gather, so the file cannot rot in
silence.

`init` scaffolds an inert `resolutions.json.sample`.

### `boundaries` — what this system talks to

**When**: "what external APIs do we call", "which env vars are secrets", "what
does it shell out to", "what endpoints does it expose". A C4-style system
context, read from `port` nodes the gather already produced.

```sh
ctx-optimize boundaries                                   # the whole surface
ctx-optimize boundaries --sensitive                       # credential NAMES only
ctx-optimize boundaries --transport network.http --direction consumes
ctx-optimize boundaries --all                             # no budget cap
ctx-optimize boundaries --json                            # otel.* under semconv names
ctx-optimize boundaries verify [--strict] [--record]      # rule drift vs a local floor
```

```
boundaries: 76 ports

CONSUMES (what this system calls out to)
  config.env     30 · 30 external · 1 SENSITIVE · 3 dynamic
      OPENROUTER_API_KEY     INFERRED   SECRET  proof/agent/agent.mjs:L28 (+1 sites)
  process.exec   12 · 12 external · 7 dynamic
      git                    AMBIGUOUS   internal/app/githistory_cli_test.go:L28 (+13 sites)

PROVIDES (what this system exposes)
  network.http   17
      /api/graph             INFERRED    internal/dashboard/dashboard.go:L119

UNRESOLVED  10 port(s) carry a dynamic identifier — os.Getenv(varName) and
            friends. The SITE is certain, the value is not; --all lists them.
```

Read it honestly:

- **A port list is a floor, not a census.** Tiers differ by rule because the
  evidence does: `config.env` is INFERRED and `process.exec` is AMBIGUOUS
  because `os.Getenv(varName)` and `exec.Command(bin)` hide the value behind a
  variable. We report the site and refuse to invent the name.
- **Secrets are NAMES.** A value is never read, stored, or printed.
- **`scope` is `external` on every consumed port today.** The join that would
  mark one `internal` compares consumed hosts against provided route paths,
  which cannot match — so `external` is not evidence that something is
  third-party, and `--where scope=internal` matches nothing.
- **`query` will not find these.** A hostname scores as prose; use this verb or
  `nodes --kind port`.
- Truncation is always stated (`… 15 more not shown (of 30)`), never silent.

In a monorepo the summary federates: one entry per identifier, carrying the
modules that reach it, rather than the same host counted once per module.

### `status` / `fresh`

**When**: "can I trust this store right now?" `status` prints store facts +
freshness vs git HEAD; `fresh` is the scriptable gate — wire it into hooks/CI
before trusting answers.

| exit | state | what it means | the fix |
|---:|---|---|---|
| 0 | fresh | store matches git HEAD | — |
| 1 | stale | the code moved on | re-gather (`add .`) |
| 2 | unknown | no git provenance | nothing is wrong; freshness just cannot be determined |
| 3 | **partial** | the last gather had producer **lanes fail**, so the store is **incomplete** | look at *why* — re-gathering may not fix it, and `up` retries a partial store in full (adapters included) rather than taking the fast path |

`partial` is a distinct code, not a reuse of `stale`, because the two need
different responses: stale means "old but complete", partial means "a producer
is missing from this graph". `status` and `fresh --json` name the failed lanes.
A hook that gates on `!= 0` keeps working unchanged.

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
| `wiki` | regenerate the deterministic markdown wiki. Every `add` does this unless the repo sets `"wiki": false` in `.ctxoptimize/config.json` — this verb always builds a **complete** one, so "off" never means "unavailable" |
| `store delete` | delete THIS repo's stores — the root store **and** every module store, at any depth, always the whole repo whichever directory you run it from. Key resolved like `add`/`status` (no path argument), so a sibling repo is never in scope. Prints the full blast radius, then **asks** `[y/N]`; off a terminal nothing is asked and nothing is deleted (`--yes` to opt in). `.ctxoptimize/` is never touched — it is committed config, not a cache. Audited |
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

### The core promise: we do not invent structure that isn't there

**If it cannot be parsed honestly, it is not indexed — and you are told to
grep.** Every abstention in this tool is one rule wearing different clothes:

| where | what we refuse to invent | what you get instead |
|---|---|---|
| a callee name defined more than once | which one this call site meant | an AMBIGUOUS shortlist + the grep that settles it |
| a method reached through a receiver we never typed | that it targets *our* method | the same, plus `unattributed callers: N` on the card |
| `#` at the start of a line in a `.txt` | that it is a heading | the file as a `document` node, and nothing else |
| a fuzzy name lookup that ties | a winner | ranked candidates and a refusal |
| a credential-shaped value | that it is safe to show | `[redacted]`, on every path |
| a gather whose producer lanes failed | that the store is complete | `fresh` exit 3, naming the lanes |

The measured case for the `.txt` rule: across **30,289 real `.txt` files in 22
repositories**, markdown parsing produced 6,902 `section` nodes of which
**95.1% were comment lines or mid-sentence prose fragments** — `#` is a comment
character in shell, python, conf, `requirements.txt`, `CMakeLists.txt`,
`robots.txt` and licence headers. Linux's 1,695 `.txt` files yielded **zero**
genuine headings. And they were not harmless: they ranked **first**, taking
26–30% of top-10 slots wherever they existed.

A threshold rule ("is `#` a comment character in *this* file?") was designed,
measured, and **rejected** — it needed two invented numbers, and a number nobody
can justify is exactly what this project refuses elsewhere. 95% junk means the
extraction is not worth having, not that it needs tuning.

The cost is stated rather than hidden: a `.txt` that genuinely *is* markdown
(`llms.txt`, prompts kept as `.txt`, a manuscript) is reachable by filename
instead of by content. Rename it `.md`, or grep it — which is what the
[tool-choice ladder](agents.md) already tells you to do for literal text.

**A smaller, predictable loss beats a heuristic that misfires in ways nobody can
enumerate.** That is the whole promise.
