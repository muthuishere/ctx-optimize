---
name: ctx-optimize
description: >
  REQUIRED before Grep/rg/Read when exploring code in any repo that contains
  a `.ctxoptimize/` directory — that marker means a pre-built knowledge graph
  of this codebase already exists, and one `ctx-optimize` call answers what a
  grep-and-read chain would: `query "<terms>"` (ranked, cited, signatures),
  `card <symbol>` (signature + doc + callers + callees, no file read),
  `affected <symbol>` (impact/blast radius), `path <a> <b>`, `explain`,
  plus a generated wiki. Use it for ANY question about code: where is X,
  how does Y work, who calls Z, what breaks if I change W, architecture,
  onboarding. Fall back to Grep/Read only for what the store lacks. Also
  builds/refreshes/shares the store ("gather this repo", "add the schema /
  kafka topics / docs", "push/pull the store"). No store yet? `ctx-optimize
  init && ctx-optimize add .` creates it in seconds. Monorepo? `ctx-optimize
  scan` finds every project — confirm the list with the user, then
  `init --scan --yes && add .` builds one store per module + a navigator.
---

# ctx-optimize

One local knowledge store per repo — code (12 embedded languages: go,
python, js, ts/tsx, java, c, c++, c#, rust, zig, sql; any other language via
a drop-in grammar pack — `<name>.wasm` + `<name>.json` in
`~/ctxoptimize/grammars/` or `.ctxoptimize/grammars/`; kotlin/swift/dart
packs ship in the repo's `grammars/`), markdown/txt docs, and anything else
via adapters — that you answer from. **Gather once, refresh cheaply, answer
from the store.**

**ctx-optimize needs no API key, no model, no database — never prompt for
one.** The binary is deterministic; you supply all semantics.

## Routing — pick the verb from the intent (huddle style: route first, then act)

| The user (or your own next step) is… | Run |
|---|---|
| Asking anything about the codebase, and a store exists | `ctx-optimize query "<question>" --json` — BEFORE any Grep/Read |
| Asking "what is X / explain X" | `ctx-optimize explain "X" --json` |
| About to open a file just to see a symbol's signature/doc/callers | `ctx-optimize card "X" --json` — the card IS the read |
| Asking "what breaks if X changes / blast radius / impact" | `ctx-optimize affected "X" --depth 2 --json` |
| Asking "how are A and B connected / trace A to B" | `ctx-optimize path "A" "B" --json` |
| Asking "what's important here / where do I start" | `ctx-optimize hubs --top 10 --json` |
| Asking to see it visually | `ctx-optimize serve` → give the printed 127.0.0.1:4747 link |
| In a repo with NO store yet | single project: `ctx-optimize init && ctx-optimize add .` (seconds). Monorepo/multi-project (workspaces, many build files): see "Multi-module flow" below — scan first, confirm with the user, then init |
| Told code changed / store looks stale | `ctx-optimize add .` (incremental: prunes deleted, re-emits changed) |
| Asked to add docs/PDF/DB/queue/logs | see "Adding content" below — each source type is different |
| Asked to share / get the team's store | `remote push` / `remote pull` (config-driven, no URL args) |
| Combining several repos/modules | `ctx-optimize merge <mod>... --into <name>` |
| Wanting a readable map of the module | open the store's `wiki/index.md` (regenerated on every `add`; `ctx-optimize wiki` to force) |
| Exporting for other tools | `ctx-optimize export --format json|dot|graphml|csv|obsidian|all` |
| Asked for a language we don't cover | `ctx-optimize languages add <name>` (kotlin, ruby, lua, swift, …— `languages list` shows all) or `languages add <github-url>`; then review the suggested .json mapping |
| Just answered a question from the store | `ctx-optimize save-result --question Q --answer A --type T --nodes "id1,id2" --outcome useful` |
| Starting a session in a repo with a store | `ctx-optimize reflect` — then read `reflections/LESSONS.md` in the store |

Fast path, imperative: **if `ctx-optimize status --json` shows nodes > 0 and
the request is a question — query. Do not rebuild. Do not grep. Do not read
files speculatively.** Need a symbol's signature, doc, or callers? `card` has it —
only open a file when a hit's `location` demands verbatim code, and then
read only that range.

## Multi-module flow (monorepos — scan, confirm, fan out)

A repo with many projects gets one store PER module plus a root **navigator**
— never one giant graph. The flow is scan → show → okay → write → add:

1. **Scan (read-only):** `ctx-optimize scan --json` (add `--depth N` if the
   tree is deep; default 5). This finds ALL projects by build-file markers.
2. **Show the user the FULL list** — every found project, not a sample. If
   they say something is missing, re-scan deeper or add globs. Never skip
   this confirmation and never silently build a single giant graph.
3. **On okay:** `ctx-optimize init --scan --yes` — writes every found module
   into `.ctxoptimize/config.json` `modules[]` (generated once; the user owns
   and edits the list after) and scaffolds the root.
4. **Gather:** `ctx-optimize add .` at the root fans out one worker per
   module in parallel (`--jobs N` to tune) and writes the navigator
   (`modules.json` + `navigator.md` in the root store).

Asking questions after that is automatic — scope follows your cwd:

- **Inside a module dir** → answers come from THAT module's store, labeled
  `[module]`. Zero hits escalate to root federation automatically.
- **At the root** → the navigator ranks modules and federates the query
  across the best-matching ones (`--modules all|a,b` to widen/pin, `--root`
  to force root scope from inside a module).
- `card X` inside a module that doesn't own X answers from the owning module
  and says so (`[not in api — found in services/worker]`).
- Cross-module edge analysis needs a merged store — offer
  `ctx-optimize merge <mod>... --into <name>` only when the user wants one
  graph; it is never automatic.
- The unified wiki starts at the root store's `wiki/index.md` (the
  navigator); each module keeps its own full wiki.

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
  recency-weighted), dead ends to avoid, and verbatim corrections. Recent
  results outweigh old ones (`--half-life-days`, default 30); a node needs
  `--min-corroboration` (default 2) distinct useful results to be preferred.

## Adding content — each source works differently (know the lanes)

- **Code + markdown/txt: automatic.** `ctx-optimize add .` — nothing to
  configure, no API key. Re-running refreshes: deleted files leave the graph
  (a >50% producer shrink is refused; add `--force` for real mass deletions).
- **Other documents (PDF, docx, URLs, wikis): YOU convert, then add.**
  There is no LLM lane and no fetcher in the binary — you are the converter.
  Turn the content into markdown (write it into the repo, e.g. `docs/`), then
  `ctx-optimize add .`. If the source must stay external, emit batch JSON
  through the door instead (below).
- **External systems (Postgres schema, Kafka topics, Redis, log shapes):
  write an adapter.** Introspect the system yourself, print ONE batch JSON to
  stdout, pipe it in: `python3 pg_schema.py | ctx-optimize add --json -`.
  The door validates fail-closed; a bad batch is rejected whole.
- **Make it repeatable — always.** A one-off pipe dies with your session.
  Save the working script as `.ctxoptimize/adapters/<name>.py` (or .js/.sh —
  extension picks the runner: node/python3/sh). Dropping the file IS the
  registration: every future `add` re-runs it. That's the refresh-the-world
  loop; leave the store refreshable, not hand-fed.

Adapter batch schema (the universal door):

```json
{
  "producer": "postgres-schema",
  "nodes": [{"id": "pg://mydb/users", "label": "users", "kind": "table",
             "file_type": "schema", "source": "pg://mydb/users"}],
  "edges": [{"source": "pg://mydb/orders", "target": "pg://mydb/users",
             "relation": "references", "confidence": "EXTRACTED"}]
}
```

Rules the door enforces: `producer` required; every node needs
`id/label/kind/file_type/source`; edge `confidence` ∈
`EXTRACTED|INFERRED|AMBIGUOUS`. Secrets in adapter scripts: read env vars by
NAME (`process.env.X` / `os.environ`), never hardcode or print values.

## Store & sync model

- Store: `~/ctxoptimize/<repo-name>/` — plain files, outside the repo.
  Override root with `--store`/`$CTX_OPTIMIZE_STORE`; folder name via
  `"name"` in config. `--path` (default cwd) picks the module.
- The ONLY thing inside the repo: committable `.ctxoptimize/` —
  `config.json` (name + remote) and `adapters/`.
- `config.json` remote: plain URL or `{"type":"s3","url":…,"credentials":
  {"access_key_id":"${TEAM_KEY_ID}", …}}`. `${VAR}` resolves from env at
  sync time — commit variable NAMES, never values; never echo resolved
  values. Omitted credentials fall back to `AWS_*` env (endpoint override
  covers R2/Hetzner/MinIO).
- `remote init <url>` writes the config (commit it — teammates clone and
  bare `remote pull` just works). `--local` keeps it per-machine instead.
- `remote push`/`pull` take NO URL — the config file is the single source of
  truth. Sync is incremental (content-hash manifest). Queries never touch
  the remote.

## Honesty rules

- Never claim a node/edge/path the CLI didn't output.
- Report counts as the CLI printed them (added/pruned/transferred).
- If the store can't answer, say what's missing and which lane would gather
  it — don't silently fall back to grepping the world.
- `path`/`explain`/`affected` accept id, exact label, or fuzzy name; if
  resolution surprises you, show the resolved node id you actually used.
