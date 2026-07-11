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

Division of labor: the `ctx-optimize` CLI does ALL deterministic work (extract,
graph, store, sync) with zero LLM/network calls; **you are the reasoning LLM on
top.** Never call a model API on the CLI's behalf — if semantic work is needed
(summaries, naming, writing an adapter), you do it yourself.

## Store model

- Central store: `~/ctxoptimize/<repo-name>/` (override root: `--store` or
  `$CTX_OPTIMIZE_STORE`; override folder name: `"name"` in ctx-optimize.json).
  The ONLY file that lives in the repo is `ctx-optimize.json`.
- Everything is plain files (ndjson/json/md) — diffable, syncable.
- Remotes are for **sync only** (S3 or a shared folder); queries always run on
  the local store. Share by pushing; a teammate who clones the repo gets the
  remote from `ctx-optimize.json` and just runs `remote pull` — no setup.

## ctx-optimize.json (repo config — commit it)

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
  },
  "adapters": [
    {"name": "kafka-topics", "run": "node hooks/kafka.js"},
    {"name": "pg-schema", "run": "python3 hooks/pg_schema.py"}
  ]
}
```

- `remote` may also be a plain string URL. `${VAR}` placeholders (in url or
  credentials) resolve from the environment at sync time — **commit variable
  NAMES, never values**; omitted credentials fall back to the standard `AWS_*`
  env vars. Never echo or write a resolved value.
- `ctx-optimize add` runs the built-in extractors AND every declared adapter
  (each command's stdout must be batch JSON, validated fail-closed). This is
  the refresh-the-world loop: one command re-gathers all declared sources.
  When you write a new adapter, save the script under `hooks/` and declare it
  here so it runs on every future `add`.

## Commands (always prefer `--json` when consuming output)

| Intent | Command |
|---|---|
| Gather everything (built-ins + declared adapters) | `ctx-optimize add <path>` |
| Feed one-off adapter output (ANY external system) | `<adapter> \| ctx-optimize add --json - --path <path>` |
| Ask the store | `ctx-optimize query "<question>" --path <path> --json` |
| Store status | `ctx-optimize status --path <path> --json` |
| Set the repo's remote (writes ctx-optimize.json) | `ctx-optimize remote init <s3://… or file:///…> --path <path>` |
| Machine-only remote (nothing in the repo) | `ctx-optimize remote init <url> --local --path <path>` |
| Publish changes | `ctx-optimize remote push --path <path> --json` |
| Fetch teammate's store | `ctx-optimize remote pull --path <path> --json` |
| Install/refresh this skill | `ctx-optimize install --skills` |

Notes:
- `--path` defaults to the current directory.
- `remote push`/`pull` take NO URL — the remote always comes from
  `ctx-optimize.json` (or the `--local` store config). Edit the file to change it.

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
script under the repo's `hooks/` dir and declare it in `ctx-optimize.json` so
every future `add` refreshes it automatically.

## Answering questions

1. `ctx-optimize query "<question>" --json` first — it returns complete hits
   (id, label, kind, source, location, neighbors) you can cite directly.
2. Only open a file if the hit's location needs verbatim code — read the
   specific range, never the whole file.
3. If the store looks stale or empty for the question, `add`/`pull` first.
