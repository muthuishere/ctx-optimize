# Custom adapters — the open door

## When and why

[Native sources](sources.md) cover databases/buckets/queues/APIs by URL.
Everything else enters through adapters: ticketing systems, log shapes,
proprietary tools, converted documents, anything that can be expressed as
nodes and edges. The binary never interprets your system — **you emit a
batch, it validates and merges**. That keeps the core deterministic while
the edges stay infinitely extensible.

## Lane 1 — drop a script (registration = the file existing)

Any `.js`/`.mjs` (node), `.py` (python3), or `.sh` file in
`.ctxoptimize/adapters/` runs on every `add .` and must print one JSON batch
to stdout. `init` scaffolds `example.js.sample` as a template — copy it:

```sh
cp .ctxoptimize/adapters/example.js.sample .ctxoptimize/adapters/ticketing.js
# edit it, then:
ctx-optimize add .              # runs it: "adapter ticketing: 2 nodes, 1 edges"
ctx-optimize adapters run       # or on demand — all scripts
ctx-optimize adapters run ticketing   # or just one, by name
```

No config entry needed. `sync` (the fast lane) skips adapter scripts —
safe, because merges are producer-scoped and their nodes stay put until the
script runs again.

### The batch shape

```json
{
  "producer": "ticketing",
  "nodes": [
    {"id": "ticket:TCK-1", "label": "TCK-1 login broken",
     "kind": "ticket", "file_type": "external", "source": "ticketing"}
  ],
  "edges": [
    {"source": "ticket:TCK-1", "target": "ticket:TCK-2",
     "relation": "relates", "confidence": "EXTRACTED"}
  ]
}
```

Validation is fail-closed: missing producer/id/label/kind, duplicate ids,
or a confidence outside `EXTRACTED|INFERRED|AMBIGUOUS` rejects the whole
batch loudly. Nothing half-lands.

## Lane 2 — the `--json` door (no file, any producer)

```sh
./my-exotic-exporter | ctx-optimize add --json -
ctx-optimize add --json facts.json
```

Same validation, upsert semantics. Use it from cron jobs, CI, other tools.

## Lane 3 — compose with native connectors (`capture` callback)

Your source needs a tunnel, an IAM role, a secrets manager, dynamic
anything? The script does its setup, sets the env var **in its own
process**, and calls the native connector back:

```js
#!/usr/bin/env node
const { execFileSync } = require("node:child_process");
// ... open tunnel, fetch credential, whatever your world needs ...
process.env.PG_TUNNEL_URL = "postgres://localhost:5433/app";
try {
  const out = execFileSync("ctx-optimize", ["capture", "PG_TUNNEL_URL"]);
  process.stdout.write(out);        // the batch flows through you
} finally {
  // ... tear the tunnel down ...
}
```

You get the connector's speed and logical-shape capture, plus your own
setup/teardown. Secrets stay in the script's process env — never on argv,
never in config, never printed.

## Documents

PDFs, wikis, exports: convert to markdown into a folder inside the repo and
`add .` — the built-in markdown producer indexes them with heading-level
sections. An adapter script that does the conversion makes it repeatable
(`adapters run docs`).

## Rules

- One producer name per adapter — that's the replace scope on re-runs
  (stale nodes from the previous run get pruned automatically).
- Deterministic output sorts diffs; timestamps in metadata, not in ids.
- Secrets by env-var NAME; the script reads its own environment at run
  time. Never bake values into the script — it's committed.
