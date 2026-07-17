# ctx-optimize — the usage card for this repo's knowledge store

A pre-built knowledge graph of this codebase lives at `~/ctxoptimize/<name>/`
(the name is in `.ctxoptimize/config.json`; default: the repo basename).
Answer questions FROM the store instead of grep-and-read chains. The binary
is deterministic — no LLM, no API key, no database; never prompt for one.
Command missing? `npm install -g @muthuishere/ctx-optimize` — or ignore this
file and read the code normally; the store is an optimization, not a
requirement.

## The front door — `ctx-optimize up`

One idempotent verb from ANY state to a store that answers: no config →
bootstrap + gather · fresh clone with a declared `remote.pull` → pull the
team's prebuilt store (gather fallback) · stale vs git HEAD → fast re-gather
· fresh → no-op. Recorded sources re-capture after the gather (24h TTL —
`--sources=always|never`; `--strict` fails on unset vars). Run it whenever;
CI gate: `up && fresh`.

## Pick by intent — the verb table

| Intent | The ONE verb |
|---|---|
| **Find** — you have words, want locations | `ctx-optimize query "<2-4 terms>" --json` |
| **Inspect** a known symbol — signature/doc/callers, no file read | `ctx-optimize card <symbol> --json` |
| **About to EDIT** — what to touch, what breaks, WHICH TESTS TO RUN | `ctx-optimize change-plan <symbol> --json` (one call replaces query+card+affected+test-grep) |
| **Blast radius** — is it safe to change | `ctx-optimize affected <symbol> --depth 2 --json` |
| **Connection** — how are A and B related | `ctx-optimize path "A" "B" --json` |
| **Orient** — where do I start | `ctx-optimize hubs --top 10 --json` |

Query with 2–4 terms, not sentences; `card` wants the exact label (query the
short name first if unsure). Output is parsed fact with exact `file:line` —
cite it directly, do NOT re-verify in source.

## Verify discipline

Before a human acts on a citation: `ctx-optimize verify "<node-id |
exact-label | file:L10-L20>"` — node exists (exact only, never fuzzy), file
exists, range in bounds, drift vs gather-time git HEAD. Exit 0 only when ALL
claims hold. A failed verify means re-query or `ctx-optimize sync` — NEVER
rephrase the claim. Fuzzy resolution announces itself (`resolved_via`) and
refuses ties with ranked candidates — pick one, don't pass `--fuzzy` on a
user's behalf.

## Tool choice — store vs grep (two-sided; wrong in either direction is the failure)

| Question shape | Tool |
|---|---|
| symbols, structure, callers, impact, architecture, "how does X work" | store verbs (table above) |
| exact literal strings, every occurrence, config VALUES, comments, member fields, build files | **grep directly — the store does not index these; say so and grep** |

The ladder: right-tool store verb first → verify before a human acts → READ
the cited range when behavior matters (that is the point of the location, not
a violation) → two store misses = switch tools, not words (`hubs`, `explain`
a neighbor, or declare the grep lane) → still nothing: abstain, naming what's
missing. The one forbidden move is stopping silently or padding from priors.

## Sources — databases, buckets, queues, external APIs

A source is an ENV VAR NAME whose value is a URL; the scheme picks the
connector (postgres, mysql, mongodb, redis, kafka, nats, s3, http(s) →
openapi, no scheme → a spec file path). The flow:

```sh
ctx-optimize adapters help postgres   # setup card: value format, credential params, paste-ready command
export BILLING_DB_URL='postgres://user:$PG_PASS@db.internal:5432/billing'
ctx-optimize add BILLING_DB_URL       # resolve → dial → capture → merge → recorded in config sources
```

- **Names only on argv** — never a raw URL with credentials on the command
  line or in committed config; literal passwords in an entry are a hard
  error. Values resolve process env → `.ctxoptimize/.env` → root `.env`
  (`.ctxoptimize/.env` is gitignored by construction — put ctx-optimize-only
  URLs like a read-only replica there).
- **Skips are normal**: a teammate without the credentials still runs `up`
  cleanly — that source reports one skip line naming the unset var, prior
  nodes stay, and they get the nodes via `remote pull`. `--strict` turns
  unset-var skips into failures (CI). `status` shows per-source staleness.
- `ctx-optimize capture <NAME>` prints one connector's Batch JSON to stdout
  without touching the store — the composition/debug primitive.
- `ctx-optimize adapters list` shows recorded sources + supported schemes +
  custom adapter scripts.
- Captures are logical shape only: system schemas skipped, partitions
  collapsed to a count on the parent, bounded samples — any cap that
  truncates is reported. Connectors live in the `ctx-optimize-adapters`
  companion binary installed beside the main one; if it's missing the error
  says so — reinstall the package, don't debug the URL.
- Exotic sources (vault-minted certs, tunnels): a script in
  `.ctxoptimize/adapters/` sets the env var in its own process and calls
  `ctx-optimize capture <NAME>` back, teardown in a `finally`.

## Sharing — remote push/pull

`remote push` / `remote pull` run the commands declared in
`.ctxoptimize/config.json` (`{"remote": {"push": "<cmd>", "pull": "<cmd>"}}`)
— the transport is the team's committed script; the binary ships none.
Scripts get `CTX_STORE_DIR` / `CTX_STORE_KEY` / `CTX_SCOPE_PREFIX` /
`CTX_DIRECTION` in env. Secrets stay env-var NAMES everywhere.

## Honesty rules

Never claim a node/edge/path the CLI didn't output; report counts as
printed; say EXTRACTED (parsed fact) vs INFERRED (name-matched) when it
matters; if the store can't answer, say what's missing and which gather lane
would fix it.
