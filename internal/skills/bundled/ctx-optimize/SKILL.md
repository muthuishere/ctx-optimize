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

- Central store: `~/.ctx-optimize/store/<module-key>/` (override: `--store` or
  `$CTX_OPTIMIZE_STORE`). The repo itself is NEVER written to — git stays clean.
- Everything is plain files (ndjson/json/md) — diffable, syncable.
- Remotes are for **sync only** (S3 or a shared folder); queries always run on
  the local store. Share by pushing, teammate pulls.

## Commands (always prefer `--json` when consuming output)

| Intent | Command |
|---|---|
| Gather a repo into the store | `ctx-optimize add <path>` |
| Feed adapter output (ANY external system) | `<adapter> \| ctx-optimize add --json - --path <path>` |
| Ask the store | `ctx-optimize query "<question>" --path <path> --json` |
| Store status | `ctx-optimize status --path <path> --json` |
| Configure a remote | `ctx-optimize remote init <s3://bucket/prefix or file:///dir> --path <path>` |
| Publish changes | `ctx-optimize remote push --path <path> --json` |
| Fetch teammate's store | `ctx-optimize remote pull --path <path> --json` |
| Install/refresh this skill | `ctx-optimize install --skills` |

Notes:
- `--path` defaults to the current directory; the store key derives from it.
- S3 remotes read standard env vars at call time: `AWS_ACCESS_KEY_ID`,
  `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, `AWS_ENDPOINT_URL` (R2/Hetzner/MinIO).
  Reference them by NAME — never echo their values.

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
edge `confidence` ∈ `EXTRACTED|INFERRED|AMBIGUOUS`. Adapters you write for a
store belong in its `hooks/` dir so they travel with push/pull. **Write new
adapters yourself when asked** ("add the kafka topics to the store") — you can
introspect anything and emit this schema.

## Answering questions

1. `ctx-optimize query "<question>" --json` first — it returns complete hits
   (id, label, kind, source, location, neighbors) you can cite directly.
2. Only open a file if the hit's location needs verbatim code — read the
   specific range, never the whole file.
3. If the store looks stale or empty for the question, `add`/`pull` first.
