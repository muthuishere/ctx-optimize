---
name: ctx-optimize
description: >
  ctx-optimize is a SHELL COMMAND (a CLI on PATH), not a callable tool: run
  every verb through your shell/bash tool, e.g. `ctx-optimize query "invoice
  tax" --json`. NEVER call a tool named ctx_optimize — no such tool exists.
  If the repo has a `.ctxoptimize/config.json` — at the root or any parent of
  your cwd (the CLI walks up to find it) — INVOKE this skill before any
  Grep/rg/Read. CONFIRM/CHECK a claim or citation — "is X really at
  file.go L10-L20?", "does this still hold?", verify before acting on a
  location — → `ctx-optimize verify "<label or file:L10-L20>"`. That file is the marker: a pre-built knowledge graph of this
  codebase already exists, so use it.
  REQUIRED before Grep/rg/Read when exploring code in any repo that contains
  a `.ctxoptimize/` directory — that marker means a pre-built knowledge graph
  of this codebase already exists, and one `ctx-optimize` call answers what a
  grep-and-read chain would: `query "<terms>"` (ranked, cited, signatures),
  `card <symbol>` (signature + doc + callers + callees, no file read),
  `change-plan <symbol>` (ONE composed answer for "I'm about to change X":
  callers + blast radius + which tests to run + confidence — use it whenever
  the intent is modifying code), `affected <symbol>` (impact/blast radius),
  `path <a> <b>`, `explain`, plus an opt-in generated wiki (`ctx-optimize wiki`). Use it for ANY question about code: where is X,
  how does Y work, who calls Z, what breaks if I change W, architecture,
  onboarding. Fall back to Grep/Read only for what the store lacks. Also
  builds/refreshes/shares the store ("gather this repo", "add the schema /
  kafka topics / docs", "push the store", "pull the store", "share the
  graph", "publish the store", "export to the team", "import/load a
  teammate's store", "sync the graph with the code"). GET A DATABASE /
  BUCKET / QUEUE / EXTERNAL API into the store — "add our postgres/mysql/
  mongo schema", "index the kafka topics / nats streams", "add the S3
  bucket", "capture the OpenAPI spec", "connect the DB" — native sources:
  an env var holding a URL is the whole contract; answer catalog/setup
  questions by RUNNING `ctx-optimize adapters list` / `adapters help
  <scheme>` and quoting the output — not from memory; deep guide: the
  skill reference sources.md (load it via this skill, not the repo cwd) (`adapters help <scheme>` → export the var →
  `ctx-optimize add <ENV_NAME>`; names only on argv, never a raw URL). NO STORE, fresh clone, or
  bare repo? ONE command: `ctx-optimize up` — bootstraps the config when none
  exists (monorepos via scan), pulls the team's prebuilt store when declared,
  gathers otherwise, no-ops when fresh. `init` is for authors wanting control. ONBOARDING a repo or
  monorepo — "set up ctx-optimize on this repo", "onboard this repo/monorepo",
  "index this project" — follow the skill reference onboarding.md (via this skill, not the repo cwd): monorepos
  `scan` first, confirm the FULL found list with the user, then
  `init --scan --yes && add .` builds one store per module + a navigator.
  Want to SEE the store or manage it visually — "open the dashboard", "see the
  graph", "manage packs/config visually", "onboard repos interactively" —
  `ctx-optimize serve` opens a local 127.0.0.1:4747 UI (Overview / Repos /
  Onboard / Query / Viewer / Settings / Changes); follow the skill reference dashboard.md (via this skill).
  ALSO the first-class helper for CUSTOMIZING extraction: "add my framework's
  routes", "extract our custom router / registerRoute", "index our k8s / helm
  / ingress", "add build-tool dependencies / gradle / pom / csproj", "support
  language X", "the graph is missing my routes/deps" — routes/manifests/
  grammar PACKS (drop-in JSON, `routes add` / `manifests add` / `languages
  add`, name or GitHub URL) plus adapters. Follow `./references/customize.md`.
  BOUNDARIES — "what external APIs do we call", "which env vars or secrets
  does this read", "what processes do we spawn", "what do we expose", "our
  egress footprint", "is anything documented but not implemented" — are
  `port` nodes: read them with `boundaries` (the answer-shaped verb —
  CONSUMES/PROVIDES with `file:line`), then `nodes --kind port`, `drift`,
  `services`. Note `query` cannot retrieve these; a hostname scores as prose.
  AUTHORING new boundary rules for a stack we don't cover is a MEASURED loop
  emitting data (`.ctxoptimize/boundaries.json`), never code, and a rule
  without its `verified` block is invalid — follow
  `./references/boundaries-authoring.md`.
---

# ctx-optimize

**HOW TO RUN IT: ctx-optimize is a CLI on PATH — every verb below is a
SHELL command. The ONLY valid execution channel is your existing shell
tool (`bash`/`shell`/`terminal`/`exec`); do NOT invent any tool or
namespace containing "ctx" (`ctx_optimize`, `ctx_optimize:add`,
`ctx-optimize.query` — all wrong). Example call: shell → `ctx-optimize
card CreateInvoice --json`.**

**ACT, don't delegate: when the store can answer, RUN the command
yourself in this turn — never hand the command back to the user, and
never ask the user for a symbol name you can `query` first.**

**The repo's `.ctxoptimize/instructions.md` is THIS repo's committed
usage card — when it exists, read it before deeper store work (verify
discipline, sources, remote push/pull) and honor any repo-specific
notes the team added outside the managed block; those customizations
override the generic guidance in this skill.**

**Wiring ctx-optimize into a SMALL-model agent runtime (gpt-4o-mini
class, toolnexus, custom loops)? The committed usage card
`.ctxoptimize/instructions.md` carries the measured-good system prompt
(section "Small models & custom runtimes") — small models skip the
store entirely without it.**

**FIRST tool call for ANY code question in a repo with `.ctxoptimize/` =
a ctx-optimize verb via shell. A grep/read BEFORE a store verb is a
routing failure. The store returned nothing? SAY so in one line — then
grep. An unknown or possibly-absent symbol → `query` it FIRST, cite the
empty result, then answer "not found" — never describe it from priors,
and never blind-grep for it before the store has spoken.**

One local knowledge store per repo that you answer from. It indexes, in v0.3,
far more than code: **source code** (12 embedded languages: go, python,
js, ts/tsx, java, c, c++, c#, rust, zig, sql; any other language via a
drop-in grammar pack — `<name>.wasm` + `<name>.json` in
`~/ctxoptimize/grammars/` or `.ctxoptimize/grammars/`; kotlin/swift/dart
packs ship in the repo's `grammars/`), **markdown/txt docs**, framework
**routes**, build-tool **dependencies** (npm/maven/gradle/go.mod/csproj/
pyproject+requirements/Cargo.toml),
**Kubernetes topology**, **config** keys, **git co-change** (which files move
together), and detected **subsystems** — plus **native sources** (postgres /
mysql / mongodb / redis / kafka / nats / s3 / mssql / OpenAPI: an env var
holding a URL is the whole contract — reference sources.md) and
anything else via adapters.
**Gather once, refresh cheaply, answer from the store.**

**ctx-optimize needs no API key, no model, no database — never prompt for
one.** The binary is deterministic; you supply all semantics.

## Pick by intent — the 5-second router (read THIS first)

Do NOT default to `query` for everything. The verb follows the intent:

| Your intent | The ONE verb |
|---|---|
| **Find** something — you have words, want locations | `ctx-optimize query "<2-4 terms>" --json` |
| **Inspect** a known symbol — signature/doc/callers, no file read | `ctx-optimize card <symbol> --json` (alias: `node`) |
| **About to EDIT** a symbol — what to touch, what breaks, WHICH TESTS TO RUN | `ctx-optimize change-plan <symbol> --json` — one call replaces query+card+affected+test-grep (~90% fewer tokens, measured) |
| **Blast radius** only — is it safe to change | `ctx-optimize affected <symbol> --depth 2 --json` (alias: `impact`) |
| **Connection** — how are A and B related | `ctx-optimize path "A" "B" --json` |
| **Orient** — where do I start in this repo | `ctx-optimize hubs --top 10 --json` |
| **Explain the whole repo** — architecture in one call | `ctx-optimize report --json` — subsystems, hubs, the seams between subsystems, and what the graph could NOT resolve. Read the gaps section before claiming any caller list is complete |
| **Need the actual code body inline** — not just the pointer | add `--include-content` to `query`/`card` — hydrates each hit's verbatim source from the file at answer time (nothing stored; default stays terse). Lines that look like credentials come back `[redacted]` — deliberate, and it applies to plain `card` too. Do NOT open the file to read around it: the point is that a secret's VALUE never enters your context. Cite the key, not the value. |
| **`card` says `unattributed callers: N`** — is the caller list complete? | It is NOT, and the line says WHY. *"defined more than once"* → several declarations share the name. *"on a receiver whose type this store never established"* → this is a **method**, and the store holds only this repo's declarations, so it can never tell `err.Error()` from a call to your own `Error`. Either way the card prints the shortlist (`edges --relation calls --confidence AMBIGUOUS --to <id>`) and the exact grep. **Run the grep** — do not report `called by` as complete when this line is present |
| **`affected` / `change-plan` on a METHOD returns few or no callers** | Expected, not a bug: cross-file method calls are attributed only when the receiver is actually tied (call written on the type, `self.`/`this.`, or the type named in the calling scope). Everything else is abstained on, so **a method's blast radius is a FLOOR** |
| **I need the abstained call sites too** | Re-run the same verb with **`--include-ambiguous`** (`card`, `explain`, `affected`, `path`, `hubs`, `change-plan`). Widened rows are marked `?` / listed under a `MAYBE` heading — treat them as candidates to verify, never as callers. Flat list instead: `edges --relation calls --confidence AMBIGUOUS --to <id>` (or `--from`). Then settle: `grep -rn '\b<Name>\b' .` for a name collision, `grep -rn '\.<Method>(' .` + check each receiver's type for an unresolved receiver |
| **About to hand a citation to a human** — does this claim still hold? | `ctx-optimize verify "<label or file:L10-L20>" --json` — node exists (exact only), file exists, range in bounds, drifted-since-gather |

If you ran `query` and then immediately wanted callers or tests — you picked
the wrong verb; the intent was edit → `change-plan`.

## The complete command surface

`./references/activation-routing.xml` is the full router: **every** ctx-optimize
verb as a `<route>` with its trigger `<when>`, `<goal>`, and exact `<cmd>` —
answer (query/card/affected/path/explain/hubs/wiki/status/fresh), build
(init/scan/add/multi-path modules), customize (routes/manifests/languages/
adapters), share (remote push/pull — YOUR committed script is the transport), export (merge/export), learn
(save-result/reflect), and manage (serve/config/log/store delete/install/update/uninstall/version).
Consult it whenever you're unsure which verb or flag fits — nothing is hidden
there. The table below is the hot path; the XML is the whole map.

## THE GATE — pick the right tool FIRST, then descend the ladder

**Tool choice, before the first call** (using the wrong tool in either
direction is the failure — forcing `query` onto literals thrashes; blind
grep on structure wastes the store):

| Question shape | Tool |
|---|---|
| symbols, structure, callers, impact, architecture, "how does X work" | store verbs (query/card/change-plan/affected/path/hubs) |
| exact literal strings, every occurrence, config VALUES, comments, member fields, build files, error-message text | **grep directly — the store does not index these; say so and grep.** Where `grep`/`rg` is absent (Windows, bare container), `ctx-optimize search '<literal>' [--ext .go] [--count]` sweeps the extractor's own file set, so a vendored tree the extractor never read cannot inflate the count |
| external hosts, env-var NAMES, spawned binaries, exposed routes — the egress/ingress surface | `ctx-optimize boundaries`. These READ like config values, but the store DOES index them as `port` nodes with `file:line`; grep finds the string, this gives direction, transport and whether it is a credential |

**The ladder — descend it, never stop on a rung:**
1. Right-tool store verb first (find→query · inspect→card ·
   edit→change-plan · impact→affected).
2. Before a human acts on a citation: `ctx-optimize verify "<label or
   file:L10-L20>"` — a failed verify means re-query or `sync`, NEVER
   rephrase the claim.
3. **When the answer depends on behavior — logic, edge cases, actual
   values — READ the cited range.** Opening the file at a store-provided
   `file:line` is the point of having the location, not a violation. What
   stays forbidden is *blind* grep-and-browse.
4. **Two store misses = switch tools, not words.** A third rephrase is
   thrash: go to `hubs`, `explain` a neighbor, or declare the grep lane
   and grep. Log the miss (`save-result --outcome dead_end`).
5. Still nothing → abstain, stating what's missing and which gather lane
   would fix it. The ONE forbidden move is stopping silently or padding
   the answer from priors.

## Routing — pick the verb from the intent (route first, then act)

| The user (or your own next step) is… | Run |
|---|---|
| Asking anything about the codebase, and a store exists | `ctx-optimize query "<question>" --json` — BEFORE any Grep/Read |
| Asking "what is X / explain X" | `ctx-optimize explain "X" --json` |
| About to open a file just to see a symbol's signature/doc/callers | `ctx-optimize card "X" --json` — the card IS the read |
| About to CHANGE a symbol ("I'm going to modify X — what do I touch, which tests do I run?") | `ctx-optimize change-plan "X" --json` — ONE composed answer (sig + callers + blast radius + tests-for + co-change + confidence); replaces the query/card/affected chain |
| Asking "what breaks if X changes / blast radius / impact" | `ctx-optimize affected "X" --depth 2 --json` |
| Asking "how are A and B connected / trace A to B" | `ctx-optimize path "A" "B" --json` |
| Asking "what's important here / where do I start" | `ctx-optimize hubs --top 10 --json` |
| **Listing / filtering by kind, relation, or metadata** — "all k8s services", "every route", "which files use react", "all resolves_to edges" — **use these, NEVER `export ... \| jq`** (native, ~in-process, works on Windows with no jq): nodes → `ctx-optimize nodes --kind service --where namespace=prod [--json\|--ndjson]`; edges → `ctx-optimize edges --relation resolves_to`. Federates across all modules at a repo root. `--select f1,f2` projects fields; `--where k=v` exact / `k~v` contains |
| **Querying an INGESTED SOURCE** — "what tables are in our DB", "what's the schema of X", "which kafka topics exist", "what endpoints does the spec declare" | `nodes --kind` over the source kinds: `database schema table view column collection key_prefix cluster topic consumer_group server stream bucket prefix api path operation securityScheme`; `edges --relation references` = the FK graph; `card <schema.table>` = columns/indexes. Do NOT re-`add` the source to answer a question. Source subgraphs are ISLANDS — there is NO code↔table/topic/endpoint edge, so never promise "which code writes this table"; deep guide `./references/sources.md` |
| **Dependencies** — "our dev deps", "who imports lodash", "external footprint with scope" | `ctx-optimize deps --scope dev` (or runtime/peer/…); `ctx-optimize deps --importers` gives dep→scope→importing-files in ONE command (replaces the multi-line `export \| jq` join). `scopes` is top-level in the JSON. Ecosystems: npm · go · maven · gradle · nuget · pypi · crates — Ruby/PHP are NOT covered (adapter door), and `(0 dependencies)` means "nothing recognized", not "none declared" |
| Need arbitrary shaping of the graph JSON | prefer the filter flags above; only if truly custom, `export --format json` then shape — but reach for `nodes`/`edges`/`deps` FIRST (faster, portable, no external tool) |
| Asking to see it visually / manage the store, packs, or config in a UI / onboard repos interactively | `ctx-optimize serve` → give the printed 127.0.0.1:4747 link; follow the skill reference dashboard.md (via this skill) |
| Repo ALREADY has a committed `.ctxoptimize/config.json` but no local store (a fresh clone — teammate already set it up) | `ctx-optimize up` — ONE command: pulls the team's prebuilt store when `remote.pull` is declared (gather fallback), gathers otherwise, no-ops when fresh. Do NOT init (author-side only; it just redirects to `up` here). |
| Setting up / onboarding a repo or monorepo (NO committed config yet, "index this repo") | fastest: `ctx-optimize up` (bootstraps + gathers in one shot; monorepos via scan, curate `.ctxoptimize/config.json` after). Wanting control / reviewing the module list first: follow the skill reference onboarding.md (via this skill, not the repo cwd) — single project: `init && add .`; monorepo: `scan` → confirm the FULL list → `init --scan --yes && add .`. `init --instructions CLAUDE\|AGENTS\|ALL\|NONE` picks which agent files get the pointer (accepts `claude.md`/`agents.md`; persists to config). Re-running `init` is safe: identical pointer content is never rewritten |
| Multi-project repo (.NET `.sln`, Gradle/Maven/Nx monorepo) or a module whose source and tests live in SEPARATE folders | Derive `modules[]` from the BUILD SYSTEM, not folders — detect it and follow the per-system parser: `./references/modules/index.md` routes to `dotnet-sln.md` / `gradle.md` / `maven.md` / `js-workspaces.md` / `naming-fallback.md`; config schema in `./references/config-json.md`. Group src+tests into one multi-path module `{"name","paths":[...]}` so test→source calls resolve |
| Told code changed / store looks stale | `ctx-optimize sync` — fast re-gather of the repo you're in (skips adapter scripts; safe, their nodes stay put). Full gather incl. adapters: `add .` |
| Asked to add a DATABASE / bucket / queue / external API ("add our postgres schema", "index the kafka topics", "capture the OpenAPI spec") | follow `./references/sources.md` — `ctx-optimize adapters help <scheme>` → `export MY_URL='...'` (value in env, root `.env`, or `~/.config/ctx-optimize/.env` — never on argv) → `ctx-optimize add MY_URL`. Recorded in config; refreshed on every `up` (24h TTL). Unset var elsewhere = a clean one-line skip, not an error |
| Asked to add docs/PDF/logs/anything non-code with NO native connector | follow `./references/adapters.md` — docs convert to markdown then `add .`; exotic systems get an adapter script, run on demand via `adapters run [name]` (dynamic creds/tunnels: the script sets the env var and calls `ctx-optimize capture <NAME>` back) |
| Wants BOUNDARIES indexed — "what external APIs do we call", "which env vars / secrets does this service read", "what does this app spawn", "our egress footprint", "add our custom HTTP wrapper / router / queue to the graph" — or asks you to AUTHOR boundary rules | follow `./references/boundaries-authoring.md`. First just look: `ctx-optimize boundaries` (CONSUMES/PROVIDES split, secrets by NAME, `file:line`), then `nodes --kind port`, `drift`, `services`. Authoring a NEW rule is an 8-step MEASURED loop (survey → propose → ground → run → measure → iterate → tier → write) emitting **data** (`.ctxoptimize/boundaries.json`), never code. **A rule without a `verified` block is invalid — by REVIEW, not by the loader**, which accepts one silently and defaults an omitted `tier` to EXTRACTED, the highest. So always set `tier` explicitly, DERIVED from recall (≥0.95 EXTRACTED · 0.70–0.95 INFERRED · <0.70 AMBIGUOUS/reject), `known_misses` mandatory. A naive regex once matched 16 of 137 real call sites and reported nothing wrong |
| Wants their FRAMEWORK ROUTES / custom router / k8s / build-tool deps / a new language indexed, or "the graph is missing my X" | follow `./references/customize.md` — check `routes/manifests/languages list` first (often already core → just `add .`); else scaffold a drop-in PACK (`routes add` / `manifests add` / `languages add`, name or github-url), edit the rule, `add .`. Read `./references/extending.md` BEFORE shipping a pack — the traps (a grammar pack can silently delete `calls` edges) |
| User says share / publish / push / pull / export to team / import / load a store — or wants sharing SET UP (github repo, s3/r2 bucket, anything) | follow `./references/push-pull.md` — the remote is a script YOU AUTHOR: arm init's `push.js.sample`/`pull.js.sample` (git lane) or write one, declare `{"remote": {"push": "<cmd>", "pull": "<cmd>"}}` in config.json, commit; then `remote push`/`pull` run it |
| Told code changed / asked about freshness ("is the graph current?") | follow `./references/sync.md` — `sync` (fast lane) / `add .` (full) / `adapters run` (slow lane); `fresh` gate. **`fresh` exit 3 = PARTIAL**: the last gather had producer LANES FAIL, so a producer (code, docs, manifests, an adapter) is MISSING from the graph — not merely out of date. Never answer from a partial store without saying so; the verdict and `--json` name which lanes failed, and re-gathering may not help if the lane is still broken |
| Combining several repos/modules into one graph | `ctx-optimize merge <mod>... --into <name>` (opt-in, never automatic) |
| Wanting a readable map of the module | open the store's `wiki/index.md` (regenerated on every `add`; `ctx-optimize wiki` to force) |
| Exporting for OTHER TOOLS (Graphviz/Gephi/Neo4j/Obsidian) | `ctx-optimize export --format json|dot|graphml|csv|obsidian|all` — for ANSWERING questions use `nodes`/`edges`/`deps` above instead, not `export \| jq` |
| Asked for a language we don't cover | `ctx-optimize languages add <name>` (kotlin, ruby, lua, swift, …— `languages list` shows all) or `languages add <github-url>`; then review the suggested .json mapping |
| Just answered a question from the store | `ctx-optimize save-result --question Q --answer A --type T --nodes "id1,id2" --outcome useful` |
| `ctx-optimize` command NOT FOUND when you try to run it | The binary isn't installed (this skill is global; the binary is separate). Tell the user: `npm install -g @muthuishere/ctx-optimize` (or download the release binary). If they can't/won't install it, DON'T loop on the error — fall back to Grep/Read; the store is an optimization, not a requirement. |
| Starting a session in a repo with a store | `ctx-optimize reflect` — then read `reflections/LESSONS.md` in the store |

Fast path, imperative: **if `ctx-optimize status --json` shows nodes > 0 and
the request is a question — query. Do not rebuild. Do not grep. Do not read
files speculatively.** Need a symbol's signature, doc, or callers? `card` has it —
only open a file when a hit's `location` demands verbatim code, and then
read only that range.

## Query craft (misses are usually phrasing, not the store)

- **Query with 2–4 terms, not sentences.** The matcher is lexical
  (IDF + prefix + trigram): `"ForwardMouseEvent RenderWidgetHost"` beats
  `"RenderWidgetHostImpl::ForwardMouseEvent definition in render_widget_host_impl.cc"`.
  Drop filler words, paths, and `::`/`.` qualifiers from queries.
- **`card` wants the node's LABEL, exactly.** Don't invent id formats
  (`content.Foo.Bar` guesses waste calls). Unsure of the label? `query` the
  short name first, copy the exact label from the hit, then `card` it.
- **Two misses = change tactics, not wording.** Rephrasing the same
  question a third time is thrash; go to `hubs`, `explain` on a neighbor,
  or the legitimate grep lanes below.
- Things the store does NOT index — use grep directly (that's lane 1/2 of
  THE GATE, say so first): member FIELDS/variables, build files
  (BUILD.gn, CMake), string literals, config values, comments.

## Answering discipline (cite or abstain)

1. `query` returns COMPLETE hits: id, label, kind, source, location,
   neighbors. Cite `source location` in your answer.
2. Answer from what the store returned. Never invent a node or an edge. Edge
   `confidence` matters: EXTRACTED is parsed fact, INFERRED is name-matched —
   say which when it matters. Resolution is honest too: `resolved_via:
   fuzzy` means the store matched a NEAR name — repeat the resolved id to
   the user; an ambiguity refusal (ranked candidates) means pick one, never
   pass `--fuzzy` silently on a user's behalf.
3. No hits? Say so, then try: different terms (the matcher does prefix +
   trigram, typos are OK), `hubs` for orientation, `explain` on a nearby
   node — or `add` if the store is stale. Never pad an answer from priors.
4. Stay in budget: `--budget N` caps output tokens (default 2000).

## Learning loop (save-result → reflect)

The store also remembers how its answers worked out — deterministically, no
model anywhere; you are the judge, the binary only tallies.

- **After answering from the store**, record the episode, citing the node ids
  you actually used:
  `ctx-optimize save-result --question "where is auth" --answer "internal/auth" --type query --nodes "auth.go::login,auth.go::verify" --outcome useful`
  Use `--outcome dead_end` when the cited nodes did NOT answer the question.
- **When an answer proved wrong**, say so with the fix:
  `ctx-optimize save-result --question "..." --outcome corrected --correction "billing actually lives in internal/pay"`
- **At session start in a repo with a store**, run `ctx-optimize reflect` and
  read `reflections/LESSONS.md` in the store: preferred nodes (corroborated,
  recency-weighted), dead ends to avoid, and verbatim corrections.

## Store lifecycle — rebuild and delete (destructive; ASK the user first)

| Intent | Command | What to know |
|---|---|---|
| **Rebuild from nothing** ("resync properly", "the store looks wrong") | `ctx-optimize add . --rebuild` | Drops the store(s) this add writes, then gathers into an empty one. Needed because `Replace` is producer-SCOPED: a **retired** producer (deleted adapter, removed pack) is never replaced, so its nodes survive every incremental gather. A normal `add` REPORTS those as `retired producer(s)`; a complete `--force` run prunes them; `--rebuild` is the certain path |
| **Delete this repo's store** | `ctx-optimize store delete` | The root store AND every module store, always the whole repo, whichever dir you run it from. Dry-run + `[y/N]` prompt by default; `--yes` for non-interactive. `.ctxoptimize/` is never touched (committed config, not a cache). Re-gather with `add .` — it takes seconds |

**Never run either on a user's behalf without asking.** They are permanent, and a
store is cheap to rebuild but the user may be mid-task. If a store looks stale or
wrong, prefer `sync` / `add .`, and mention `--rebuild` as an option rather than
taking it.

## Settling an abstention permanently — `.ctxoptimize/resolutions.json`

When `card` reports `unattributed callers` for a **method** whose receiver could
not be typed, and the user confirms the name belongs to a type they do NOT own
(`Error`, `String`, `Close` on stdlib/dependency types), that answer can be
written down instead of re-derived every session:

```json
{ "external_methods": ["Error", "String", "Close"] }
```

Committed, so the whole team's agents inherit it. It only ever RETIRES a
shortlist — it never creates a call edge and never deletes a resolved one — so a
wrong entry costs recall and can never make the graph wrong. A declared name that
matches nothing is reported on every gather.

**Propose it, do not write it unasked**: it is an assertion about the user's code,
and they are the one who knows whether that method is theirs.

## The core promise — we do not invent structure that isn't there

**If it cannot be parsed honestly, it is not in the store — and the store says
so.** Every abstention you will meet is this one rule:

- a name defined more than once → AMBIGUOUS shortlist, not a guess
- a method whose receiver was never typed → `unattributed callers: N`, not a caller list
- a fuzzy tie → ranked candidates and a refusal, not `matches[0]`
- a credential-shaped value → `[redacted]`, on every path
- a failed producer lane → `fresh` exit 3, not "fresh"
- **`#` at the start of a line in a `.txt` → a comment, not a heading.** A `.txt`
  yields ONE node: the file. No sections. Measured over 30,289 real `.txt` files:
  95.1% of the "headings" markdown parsing found there were comment lines or
  prose fragments, and they ranked FIRST — 26–30% of top-10 slots.

**So when a question is about the CONTENT of a `.txt`, grep it and say so.** The
store deliberately holds only the filename. Same for `LICENSE.txt`,
`requirements.txt` comments, `CMakeLists.txt`, Unicode data files, `*-expected.txt`
baselines. This is not a gap to work around silently — it is the promise, and
routing to grep is the correct behaviour, not a fallback you should apologise for.

## Honesty rules

- Never claim a node/edge/path the CLI didn't output.
- Report counts as the CLI printed them (added/pruned/transferred).
- If the store can't answer, say what's missing and which lane would gather
  it — don't silently fall back to grepping the world.
- `path`/`explain`/`affected` accept id, exact label, or fuzzy name; if
  resolution surprises you, show the resolved node id you actually used.

## Deep guides

- `./references/onboarding.md` — set up a store on a NEW repo or monorepo:
  scan → confirm the full module list → `init --scan --yes && add .`, verify
- `./references/config-json.md` — the `.ctxoptimize/config.json` contract:
  full schema, the two module shapes, how to author it (a separate step from add)
- `./references/modules/` — multi-project layout, one parser asset per build
  system: `index.md` (detect+dispatch) → `dotnet-sln.md`, `gradle.md`,
  `maven.md`, `js-workspaces.md`, `naming-fallback.md`
- `./references/dashboard.md` — `serve` as the visual management surface:
  Overview / Repos (onboard, re-gather, remove) / Query / Viewer / Settings
  (packs + config) / Changes (audit); local, read-safe, mutations audited
- `./references/customize.md` — make extraction fit ANY stack: framework
  routes, custom routers, k8s, build-tool deps, new languages — core vs
  drop-in packs (`routes/manifests/languages add`), the pack doctrine
- `./references/multi-module.md` — monorepos: scan → confirm → fan-out add,
  navigator, scope-follows-cwd querying, merge policy
- `./references/sources.md` — native sources: databases/buckets/queues/APIs
  by env-var name (URL scheme picks the connector), the env-var-only rule +
  `.env` ladder, skip semantics + staleness, `capture` as the debug
  primitive, the logical-shape promise, the `ctx-optimize-adapters`
  companion binary — PLUS how to QUERY a captured source (the kind
  vocabulary), why source subgraphs are islands, and the spec-format limits
- `./references/boundaries-authoring.md` — BOUNDARIES (`port` nodes: env vars,
  spawned processes, HTTP hosts, routes served, queues, storage keys): how to
  read them, and the measured 8-step loop for authoring rules in
  `.ctxoptimize/boundaries.json` — the real schema, the tier-from-recall law,
  the `verified` block, `boundaries verify` as the standing check, and the two
  failures that made measurement mandatory (12% recall through a wrapper; a
  vendored corpus reported as production egress)
- `./references/extending.md` — the TRAPS in the extension doors, read before
  shipping a pack: a grammar pack silently deletes `calls` edges when its decl
  names collide (measured 126 lost + 1 invented on beam); manifest selectors
  are root-anchored, exact-depth, no descendant operator, `emit` only
  dependency|task; `deps` ecosystem coverage (Ruby/PHP absent, locks skipped)
- `./references/adapters.md` — everything beyond code + markdown with no
  native connector: doc conversion lane, hand-authored batch emitters (the
  escape hatch), the callback pattern for dynamic creds/tunnels, the batch
  schema
- `./references/sync.md` — sync = keep the graph matching the code: `sync`
  fast lane (no adapter scripts) · `add .` full gather · `adapters run` slow
  lane · `fresh` gate
- `./references/push-pull.md` — share/publish/import the store across the team
