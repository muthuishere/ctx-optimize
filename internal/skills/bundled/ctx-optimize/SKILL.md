---
name: ctx-optimize
description: >
  If the repo has a `.ctxoptimize/config.json` — at the root or any parent of
  your cwd (the CLI walks up to find it) — INVOKE this skill before any
  Grep/rg/Read. That file is the marker: a pre-built knowledge graph of this
  codebase already exists, so use it.
  REQUIRED before Grep/rg/Read when exploring code in any repo that contains
  a `.ctxoptimize/` directory — that marker means a pre-built knowledge graph
  of this codebase already exists, and one `ctx-optimize` call answers what a
  grep-and-read chain would: `query "<terms>"` (ranked, cited, signatures),
  `card <symbol>` (signature + doc + callers + callees, no file read),
  `change-plan <symbol>` (ONE composed answer for "I'm about to change X":
  callers + blast radius + which tests to run + confidence — use it whenever
  the intent is modifying code), `affected <symbol>` (impact/blast radius),
  `path <a> <b>`, `explain`, plus a generated wiki. Use it for ANY question about code: where is X,
  how does Y work, who calls Z, what breaks if I change W, architecture,
  onboarding. Fall back to Grep/Read only for what the store lacks. Also
  builds/refreshes/shares the store ("gather this repo", "add the schema /
  kafka topics / docs", "push the store", "pull the store", "share the
  graph", "publish the store", "export to the team", "import/load a
  teammate's store", "sync the graph with the code"). NO STORE, fresh clone, or
  bare repo? ONE command: `ctx-optimize up` — bootstraps the config when none
  exists (monorepos via scan), pulls the team's prebuilt store when declared,
  gathers otherwise, no-ops when fresh. `init` is for authors wanting control. ONBOARDING a repo or
  monorepo — "set up ctx-optimize on this repo", "onboard this repo/monorepo",
  "index this project" — follow `./references/onboarding.md`: monorepos
  `scan` first, confirm the FULL found list with the user, then
  `init --scan --yes && add .` builds one store per module + a navigator.
  Want to SEE the store or manage it visually — "open the dashboard", "see the
  graph", "manage packs/config visually", "onboard repos interactively" —
  `ctx-optimize serve` opens a local 127.0.0.1:4747 UI (Overview / Repos /
  Onboard / Query / Viewer / Settings / Changes); follow `./references/dashboard.md`.
  ALSO the first-class helper for CUSTOMIZING extraction: "add my framework's
  routes", "extract our custom router / registerRoute", "index our k8s / helm
  / ingress", "add build-tool dependencies / gradle / pom / csproj", "support
  language X", "the graph is missing my routes/deps" — routes/manifests/
  grammar PACKS (drop-in JSON, `routes add` / `manifests add` / `languages
  add`, name or GitHub URL) plus adapters. Follow `./references/customize.md`.
---

# ctx-optimize

One local knowledge store per repo that you answer from. It indexes, in v0.3,
far more than code: **source code** (12 embedded languages: go, python,
js, ts/tsx, java, c, c++, c#, rust, zig, sql; any other language via a
drop-in grammar pack — `<name>.wasm` + `<name>.json` in
`~/ctxoptimize/grammars/` or `.ctxoptimize/grammars/`; kotlin/swift/dart
packs ship in the repo's `grammars/`), **markdown/txt docs**, framework
**routes**, build-tool **dependencies** (npm/maven/gradle/go.mod/csproj),
**Kubernetes topology**, **config** keys, **git co-change** (which files move
together), and detected **subsystems** — plus anything else via adapters.
**Gather once, refresh cheaply, answer from the store.**

**ctx-optimize needs no API key, no model, no database — never prompt for
one.** The binary is deterministic; you supply all semantics.

## Pick by intent — the 5-second router (read THIS first)

Do NOT default to `query` for everything. The verb follows the intent:

| Your intent | The ONE verb |
|---|---|
| **Find** something — you have words, want locations | `ctx-optimize query "<2-4 terms>" --json` |
| **Inspect** a known symbol — signature/doc/callers, no file read | `ctx-optimize card <symbol> --json` |
| **About to EDIT** a symbol — what to touch, what breaks, WHICH TESTS TO RUN | `ctx-optimize change-plan <symbol> --json` — one call replaces query+card+affected+test-grep (~90% fewer tokens, measured) |
| **Blast radius** only — is it safe to change | `ctx-optimize affected <symbol> --depth 2 --json` |
| **Connection** — how are A and B related | `ctx-optimize path "A" "B" --json` |
| **Orient** — where do I start in this repo | `ctx-optimize hubs --top 10 --json` |

If you ran `query` and then immediately wanted callers or tests — you picked
the wrong verb; the intent was edit → `change-plan`.

## The complete command surface

`./references/activation-routing.xml` is the full router: **every** ctx-optimize
verb as a `<route>` with its trigger `<when>`, `<goal>`, and exact `<cmd>` —
answer (query/card/affected/path/explain/hubs/wiki/status/fresh), build
(init/scan/add/multi-path modules), customize (routes/manifests/languages/
adapters), share (remote push/pull — YOUR committed script is the transport), export (merge/export), learn
(save-result/reflect), and manage (serve/config/log/install/update/uninstall/version).
Consult it whenever you're unsure which verb or flag fits — nothing is hidden
there. The table below is the hot path; the XML is the whole map.

## THE GATE — every search goes through the store first (non-negotiable)

In a repo with `.ctxoptimize/`, ctx-optimize IS the search tool. Before ANY
Grep, rg, Glob, find, or exploratory Read — run the verb the intent map above
picks (find→query · inspect→card · edit→change-plan · impact→affected). No
exceptions for "just a quick grep": the quick grep is exactly the cost this
store exists to kill, and skipping the store means the answer arrives without
citations.

Grep/Read are permitted ONLY as:
1. **Exhaustive literal sweeps** — every occurrence of an exact string
   (renames, license headers). That is grep's job; say so and grep.
2. **Store-miss fallback** — the store returned nothing relevant AND you
   said so first (see answering discipline), AND `add .` wouldn't fix it.
3. **Verbatim body reads** the store already located — open ONLY the cited
   `source location` range, never browse around it.

If you notice you grepped first anyway: stop, run the store query, and
record the episode with `save-result --outcome dead_end` so the miss is
counted honestly.

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
| Asking to see it visually / manage the store, packs, or config in a UI / onboard repos interactively | `ctx-optimize serve` → give the printed 127.0.0.1:4747 link; follow `./references/dashboard.md` |
| Repo ALREADY has a committed `.ctxoptimize/config.json` but no local store (a fresh clone — teammate already set it up) | `ctx-optimize up` — ONE command: pulls the team's prebuilt store when `remote.pull` is declared (gather fallback), gathers otherwise, no-ops when fresh. Do NOT init (author-side only; it just redirects to `up` here). |
| Setting up / onboarding a repo or monorepo (NO committed config yet, "index this repo") | fastest: `ctx-optimize up` (bootstraps + gathers in one shot; monorepos via scan, curate `.ctxoptimize/config.json` after). Wanting control / reviewing the module list first: follow `./references/onboarding.md` — single project: `init && add .`; monorepo: `scan` → confirm the FULL list → `init --scan --yes && add .`. `init --instructions CLAUDE\|AGENTS\|ALL\|NONE` picks which agent files get the pointer (accepts `claude.md`/`agents.md`; persists to config). Re-running `init` is safe: identical pointer content is never rewritten |
| Multi-project repo (.NET `.sln`, Gradle/Maven/Nx monorepo) or a module whose source and tests live in SEPARATE folders | Derive `modules[]` from the BUILD SYSTEM, not folders — detect it and follow the per-system parser: `./references/modules/index.md` routes to `dotnet-sln.md` / `gradle.md` / `maven.md` / `js-workspaces.md` / `naming-fallback.md`; config schema in `./references/config-json.md`. Group src+tests into one multi-path module `{"name","paths":[...]}` so test→source calls resolve |
| Told code changed / store looks stale | `ctx-optimize sync` — fast re-gather of the repo you're in (skips adapter scripts; safe, their nodes stay put). Full gather incl. adapters: `add .` |
| Asked to add docs/PDF/DB/queue/logs/anything non-code | follow `./references/adapters.md` — docs convert to markdown then `add .`; systems get an adapter script, run on demand via `adapters run [name]` |
| Wants their FRAMEWORK ROUTES / custom router / k8s / build-tool deps / a new language indexed, or "the graph is missing my X" | follow `./references/customize.md` — check `routes/manifests/languages list` first (often already core → just `add .`); else scaffold a drop-in PACK (`routes add` / `manifests add` / `languages add`, name or github-url), edit the rule, `add .` |
| User says share / publish / push / pull / export to team / import / load a store — or wants sharing SET UP (github repo, s3/r2 bucket, anything) | follow `./references/push-pull.md` — the remote is a script YOU AUTHOR: arm init's `push.js.sample`/`pull.js.sample` (git lane) or write one, declare `{"remote": {"push": "<cmd>", "pull": "<cmd>"}}` in config.json, commit; then `remote push`/`pull` run it |
| Told code changed / asked about freshness ("is the graph current?") | follow `./references/sync.md` — `sync` (fast lane) / `add .` (full) / `adapters run` (slow lane); `fresh` gate |
| Combining several repos/modules into one graph | `ctx-optimize merge <mod>... --into <name>` (opt-in, never automatic) |
| Wanting a readable map of the module | open the store's `wiki/index.md` (regenerated on every `add`; `ctx-optimize wiki` to force) |
| Exporting for other tools | `ctx-optimize export --format json|dot|graphml|csv|obsidian|all` |
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
   say which when it matters.
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
- `./references/adapters.md` — everything beyond code + markdown: doc
  conversion lane, system adapters, the batch schema
- `./references/sync.md` — sync = keep the graph matching the code: `sync`
  fast lane (no adapter scripts) · `add .` full gather · `adapters run` slow
  lane · `fresh` gate
- `./references/push-pull.md` — share/publish/import the store across the team
