# Adding content beyond code and markdown — adapters are the one door

The binary reads source code (12 embedded languages + grammar packs) and
`.md`/`.txt`. Everything else enters through adapters — scripts YOU write
that print one batch JSON to stdout. No LLM lane, no fetcher, no DB driver
in the binary, ever.

## Documents (PDF, docx, wiki pages, anything human-authored)

Simplest lane: convert to markdown, drop it in the repo (e.g. `docs/`), run
`ctx-optimize add .`. You are the converter — use whatever turns the source
into clean markdown, keep headings (they become section nodes). If the
source must stay external, emit a batch through the door instead.

## Systems (Postgres schema, Kafka topics, Redis, log shapes, anything live)

Introspect the system yourself, print ONE batch, pipe it in:

```
python3 pg_schema.py | ctx-optimize add --json -
```

The door validates fail-closed; a bad batch is rejected whole.

## Make it repeatable — always

A one-off pipe dies with your session. Save the working script as
`.ctxoptimize/adapters/<name>.py` (or .js/.sh — extension picks the runner:
node/python3/sh). Dropping the file IS the registration: every future
`add .` re-runs it alongside the code and markdown producers. That's the
refresh-the-world loop — leave the store refreshable, not hand-fed.

## Running adapters on demand (the slow lane)

Adapter scripts can be arbitrarily slow, so the fast sync skips them:

- `ctx-optimize sync` (and `add . --no-adapters`) re-gathers code/docs/
  manifests/git WITHOUT running scripts — safe, adapter nodes stay put
  (replace is producer-scoped).
- `ctx-optimize adapters list` shows every adapter (declared in config.json
  + discovered scripts, declared names winning).
- `ctx-optimize adapters run` re-runs them all; `adapters run <name>` just
  one. Run it when the external system changed, not on every code edit.
  Running one adapter never disturbs the code graph.

## Batch schema (the universal door)

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
