# Native sources — databases, buckets, queues, external APIs by env-var name

**A source is an environment variable name. Its value is a URL. The URL
scheme picks the connector.** That convention is the whole design — no
adapter script to author, no per-ecosystem CLI on the machine.

## The flow (route every "get a database/bucket/queue/API into the store" here)

```sh
ctx-optimize adapters help postgres    # 1. the setup card for the scheme
export BILLING_DB_URL='postgres://user:$PG_PASS@db.internal:5432/billing'   # 2. value in env (or a .env file — see ladder)
ctx-optimize add BILLING_DB_URL        # 3. resolve → dial → capture → merge → recorded in config sources
```

`add <ENV_NAME>` records the name in `.ctxoptimize/config.json` `sources` on
a successful capture — from then on every `ctx-optimize up` refreshes it
(24h TTL; `--sources=always|never` overrides). Commit the config; the value
never leaves the machine that holds it.

Schemes: `postgres(ql)` · `mysql` · `mongodb(+srv)` · `redis(s)` · `kafka`
· `nats` · `s3` (MinIO/R2 via a dotted/ported endpoint host; bare
`s3://bucket/prefix` uses the AWS credential chain) · `http(s)` → openapi
(must parse as a spec) · no scheme → an OpenAPI spec FILE path · `mssql`.
`ctx-optimize adapters help <scheme>` prints the exact value format,
credential/cert params, percent-encoding hints, and the paste-ready
commands — generated from the connector's own parameter table, never
hand-written. `adapters list` shows recorded sources + supported schemes.

## The env-var-only rule (hard, enforced)

- **argv takes NAMES only** (`^[A-Z_][A-Z0-9_]*$`) — never paste a URL with
  credentials on a command line or into config. A literal password in a
  committed entry is a hard error at load ("credentials belong in env
  vars"); literal usernames are fine. Config entries may be a bare name, a
  `$NAME`, or a URL template with embedded `$VARs`
  (`postgres://$PG_USER:$PG_PASS@db.internal:5432/billing`).
- **Resolution ladder**: process env → repo-root `.env` →
  `~/.config/ctx-optimize/.env` (specific over general; real env wins for
  CI/prod). The machine-global `~/.config/ctx-optimize/.env` is for
  credentials shared across every repo on this machine — it lives outside
  any repo, so it can never be committed. NEVER read any `.env` yourself —
  the binary resolves names internally so secret values stay out of model
  context. A TRACKED root `.env` triggers a loud warning
  (`git rm --cached` it).
- Every output is scrubbed: summaries, errors, ids, the store itself carry
  at most `postgres://user:***@host/db`. Don't try to "check" a value.

## Skip semantics + staleness (skips are normal, not errors)

Per-source outcomes are exactly three: **captured** · **skipped** (a
referenced var unset, or fresh within TTL, or `--sources=never`) ·
**failed** (dial/parse error — prior nodes KEPT, reported loudly). A
teammate without credentials still runs `up` cleanly: the source prints one
skip line naming the unset var, and they get the nodes via `remote pull`.
`--strict` turns unset-var skips into failures (CI). `status`/`up` print
per-source staleness (`BILLING_DB_URL captured 2h ago`); sources no longer
declared in config are reported as orphans (`up --prune-sources` removes
their nodes).

## What a capture contains — the logical-shape promise

Connectors capture the LOGICAL shape a developer reasons about, never
physical/instance data: system schemas/dbs/topics skipped (`pg_*`,
`information_schema`, `__consumer_offsets`, `$SYS.*`, …), a partitioned
table is ONE node with `partitions: N` (chunks/children never enumerated),
redis is a bounded SCAN summarized by key-prefix pattern, s3 lists
prefixes only (depth-capped), mongo fields come from a capped sample. Any
cap that truncates is REPORTED in the summary line — silent truncation
never reads as full coverage. Measured: a 100-table/3-schema postgres
captures in ~31 ms including connect.

## `capture` — the debug/composition primitive

`ctx-optimize capture <ENV_NAME>` dials ONE source and prints the Batch
JSON to stdout WITHOUT touching the store. Use it to inspect what a
connector would emit, to debug a failing source, or from adapter scripts
(callback pattern below).

## The companion binary

Connector drivers live in **`ctx-optimize-adapters`**, installed beside the
main binary in every archive/npm package (the main binary stays driver-free
and hot-path-fast; it execs the sibling only when a source dials). If a dial
errors with the companion binary's name: the installation is incomplete —
reinstall the package; do not debug the URL.

## Dynamic/exotic sources — the adapter-callback pattern

Tunnels, vault-minted credentials, custom headers: an ordinary adapter
script sets the env var IN ITS OWN PROCESS and calls `capture` back;
teardown is a plain `finally`:

```js
// .ctxoptimize/adapters/prod-db.js — auto-discovered at gather
const { execSync } = require("node:child_process");
openTunnel();
try {
  process.env.PG_TUNNEL_URL = "postgres://localhost:5433/app";
  process.stdout.write(execSync("ctx-optimize capture PG_TUNNEL_URL"));
} finally { closeTunnel(); }
```

Source types with no connector at all: hand-author a batch-emitting adapter
script (`./adapters.md`) — that lane remains the escape hatch.
