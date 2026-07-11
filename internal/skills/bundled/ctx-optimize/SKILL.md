---
name: ctx-optimize
description: >
  Answer codebase and system questions from a pre-built local knowledge store
  instead of grepping and reading files — optimize the context an agent spends
  on code. Backed by the `ctx-optimize` CLI (Go, deterministic, no LLM, no DB,
  no network at query time); YOU (the agent) are the only intelligence. Trigger
  on: "use ctx-optimize", "build the code graph", "query the graph", "what is
  in this codebase", "gather this repo", "add this repo to the store",
  "refresh the store", "push/pull the store", "share the graph with the team",
  "add the database schema / kafka topics / logs to the store", or when a repo
  question can be answered from an existing store instead of searching files.
---

# ctx-optimize

Turns a repo (and, via adapters, DB schemas, messaging topics, log shapes,
documents) into ONE local knowledge store you answer from. **Gather once,
refresh cheaply, answer from the store — never go everywhere every time.**

Built-in code extraction (tree-sitter, no setup): Go, Python, JavaScript,
TypeScript/TSX, Java, C, C++, C#, Rust — functions/methods/classes/structs/
interfaces/enums/traits with locations, contains + imports edges, and
name-resolved call edges (INFERRED). Markdown/txt docs land in the same
graph. `add` on a 4k-file repo takes well under a second.

Division of labor: the `ctx-optimize` CLI does ALL deterministic work (extract,
graph, store, sync) with zero LLM/network calls; **you are the reasoning LLM on
top.** Never call a model API on the CLI's behalf — if semantic work is needed
(summaries, naming, writing an adapter), you do it yourself.

## Store model

- Central store: `~/ctxoptimize/<repo-name>/` (override root: `--store` or
  `$CTX_OPTIMIZE_STORE`; override folder name: `"name"` in config). The ONLY
  thing that lives in the repo is the committable `.ctxoptimize/` directory.
- Everything is plain files (ndjson/json/md) — diffable, syncable.
- Remotes are for **sync only** (S3 or a shared folder); queries always run on
  the local store. Share by pushing; a teammate who clones the repo gets the
  remote from `.ctxoptimize/config.json` and just runs `remote pull` — no setup.

## .ctxoptimize/ (repo config — commit the whole directory)

```
.ctxoptimize/
  config.json     name + remote (see below)
  adapters/       drop scripts here; every .js/.py/.sh runs on `add`
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

- `remote` may also be a plain string URL. `${VAR}` placeholders (in url or
  credentials) resolve from the environment at sync time — **commit variable
  NAMES, never values**; omitted credentials fall back to the standard `AWS_*`
  env vars. Never echo or write a resolved value.
- **Adapters:** dropping a script into `.ctxoptimize/adapters/` IS the
  registration — `.js`/`.mjs` run via node, `.py` via python3, `.sh` via sh;
  other extensions are inert (the scaffold ships `example.js.sample` as a
  template). Each script prints ONE batch JSON to stdout, validated
  fail-closed. `ctx-optimize add` = built-in extractors + every adapter
  script: one command refreshes the whole world. When you write a new adapter
  for the user, save it there so every future `add` re-gathers it.

## Commands (always prefer `--json` when consuming output)

| Intent | Command |
|---|---|
| First time in a repo (scaffolds .ctxoptimize/) | `ctx-optimize init --path <path>` |
| Gather everything (built-ins + adapter scripts) | `ctx-optimize add <path>` |
| Feed one-off adapter output (ANY external system) | `<adapter> \| ctx-optimize add --json - --path <path>` |
| Ask the store | `ctx-optimize query "<question>" --path <path> --json` |
| How are A and B connected? | `ctx-optimize path "A" "B" --path <path> --json` |
| What is X? (node + neighborhood) | `ctx-optimize explain "X" --path <path> --json` |
| What breaks if X changes? (blast radius) | `ctx-optimize affected "X" --depth 2 --path <path> --json` |
| Most important nodes (god nodes) | `ctx-optimize hubs --top 10 --path <path> --json` |
| Store status | `ctx-optimize status --path <path> --json` |
| Combine modules into one view | `ctx-optimize merge <module\|path>... --into <name>` |
| Dump the graph for other tools | `ctx-optimize export --format json\|dot --path <path>` |
| Show the store visually (user asks "show me the graph") | `ctx-optimize serve --path <path>` → open the printed http://127.0.0.1:4747 link |
| Set the repo's remote (writes .ctxoptimize/config.json) | `ctx-optimize remote init <s3://… or file:///…> --path <path>` |
| Machine-only remote (nothing in the repo) | `ctx-optimize remote init <url> --local --path <path>` |
| Publish changes | `ctx-optimize remote push --path <path> --json` |
| Fetch teammate's store | `ctx-optimize remote pull --path <path> --json` |
| Install/refresh this skill | `ctx-optimize install --skills` |

Notes:
- `--path` defaults to the current directory.
- Re-running `add` REPLACES each producer's world: nodes whose source is gone
  are pruned automatically. A shrink to under half a producer's nodes is
  refused (broken-run guard) — pass `--force` when it's a real mass deletion.
- `path`/`explain`/`affected` accept a node id, exact label, or fuzzy name.
- `remote push`/`pull` take NO URL — the remote always comes from
  `.ctxoptimize/config.json` (or the `--local` store config). To change the
  remote, edit the file (or re-run `remote init`), never pass a URL to sync.

## Writing an adapter (the open door)

Any system can be gathered. Emit this JSON to stdout and pipe into the door:

```json
{
  "producer": "postgres-schema",
  "nodes": [{"id": "pg://mydb/users", "label": "users", "kind": "table",
             "file_type": "schema", "source": "pg://mydb/users"}],
  "edges": [{"source": "pg://mydb/orders", "target": "pg://mydb/users",
             "relation": "references", "confidence": "EXTRACTED"}]
}
```

Rules the door enforces (it rejects the whole batch otherwise): `producer`
required (provenance); every node needs `id/label/kind/file_type/source`;
edge `confidence` ∈ `EXTRACTED|INFERRED|AMBIGUOUS`. **Write new adapters
yourself when asked** ("add the kafka topics to the store") — you can
introspect anything and emit this schema. Then make it repeatable: save the
script as `.ctxoptimize/adapters/<name>.js` (or .py/.sh) so every future
`add` refreshes it automatically. Secrets inside adapter scripts follow the
same rule: read env vars by name, never hardcode values.

## Answering questions

1. `ctx-optimize query "<question>" --json` first — it returns complete hits
   (id, label, kind, source, location, neighbors) you can cite directly.
2. Only open a file if the hit's location needs verbatim code — read the
   specific range, never the whole file.
3. If the store looks stale or empty for the question, `add`/`pull` first.
