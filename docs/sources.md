# Native sources — databases, buckets, queues, APIs by env-var name

## The whole contract in one line

**An environment variable holds a URL; the URL's scheme picks the
connector.**

```sh
ctx-optimize adapters help postgres     # 1. the setup card for the scheme
export BILLING_DB_URL='postgres://reader:$PG_PASS@db.internal:5432/billing'
ctx-optimize add BILLING_DB_URL         # 2. dial, capture, merge, record
```

The store now answers "what tables does billing have", "which topics exist",
"what's under the docs bucket" — alongside your code. Recorded sources
refresh on every `up` (24h TTL).

## When and why

- **When**: your agent (or you) keeps asking about schemas, topics, buckets,
  or an external API — knowledge that lives outside the repo.
- **Why native**: agent-authored dump scripts are slow and drift. The
  connectors are wire-protocol Go — a real 100-table postgres captures in
  ~31ms — and capture the **logical shape**: system schemas skipped,
  partitions collapsed to a `partitions:N` fact on the parent, samples
  bounded, every cap reported.

## Supported schemes

`postgres(ql)` · `mysql` · `mongodb(+srv)` · `redis(s)` · `kafka` · `nats` ·
`s3` · `sqlserver/mssql` · `http(s)` (OpenAPI) · a bare file path (local
OpenAPI spec). `ctx-optimize adapters help <scheme>` prints the value
format, credential params, percent-encoding hints, and a paste-ready
command. Drivers live in the `ctx-optimize-adapters` companion binary
(installed beside the main one automatically) so the query path stays fast.

## Credentials — the hard rules

- **Names only on argv and in committed config, never a raw URL.** A literal
  password in a committed entry is a hard error at load.
- Templates with embedded vars are fine, including folded into one var:
  `export DOCS_S3_URL='s3://$MINIO_KEY:$MINIO_SECRET@minio.internal:9000/docs'`
- **Resolution ladder** (specific over general): process env → repo-root
  `.env` → `~/.config/ctx-optimize/.env`. The machine-global file is for
  URLs shared across every repo on this machine (a personal dev DB, a local
  MinIO) — it lives outside any repo, so it can never be committed. A
  TRACKED root `.env` triggers a loud warning (`git rm --cached` it).
- **Every output is scrubbed**: summaries, errors, ids, and the store carry
  at most `postgres://user:***@host/db`. Values are never written or logged.

## Teams: skips are normal

Declare sources in committed config (recording happens automatically on a
successful `add NAME`). A teammate without the credentials still runs `up`
cleanly — that source reports one skip line naming the unset var, prior
nodes stay, and they get the data via `remote pull`. `--strict` turns
unset-var skips into failures (for CI). `status` shows per-source staleness.

## Debugging

```sh
ctx-optimize capture BILLING_DB_URL   # dial + print Batch JSON, no store write
ctx-optimize up --sources=always      # force re-capture ignoring the TTL
ctx-optimize up --prune-sources       # drop producers no longer declared
```

## Beyond the built-ins

Dynamic credentials, tunnels, IAM roles, or systems with no native scheme:
write an [adapter script](adapters.md) — it sets the env var in its own
process and calls `ctx-optimize capture <NAME>` back, or emits batch JSON
itself.
