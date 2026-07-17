# ADR — native sources: an env var holding a URL is the whole contract

Status: PROPOSED (converged 2026-07-17 over live discussion; final
direction: "make it one, and no direct — just environment variables
only"). This file supersedes ALL earlier drafts of this change (JS
templates shelling to CLIs; type+env maps; script lanes with
setup/resolve/teardown; the `$(cmd)` value ladder) — each rejected below.

## Context

The adapter pipeline works (DiscoverAdapters → runAdapter → validated
Batch merge) but AUTHORING an emitter is slow: an agent hand-crafts JSON
per repo from references/adapters.md. Teams want databases, buckets,
queues, and external APIs in the store with one declaration — including
enterprises with a dozen connection strings.

## The convention (the whole design)

**A source is an environment variable name. Its value is a URL. The URL
scheme picks the connector.**

```json
{"name": "billing",
 "sources": ["BILLING_DB_URL", "ORDERS_DB_URL", "CACHE_REDIS_URL",
             "EVENTS_KAFKA_URL", "DOCS_S3_URL", "PARTNER_API_SPEC"]}
```

A source entry takes three forms (directed 2026-07-17: "allow all" —
bare name, $-form, or a template with embedded $VARs):

```json
{"sources": [
  "DATABASE_URL",
  "$ORDERS_DB_URL",
  "postgres://$PG_USER:$PG_PASS@db.internal:5432/billing",
  "s3://$MINIO_KEY:$MINIO_SECRET@minio.internal:9000/docs"
]}
```

Expansion is plain `$VAR` substitution, in memory, at dial time.
**Resolution order: process environment → `.ctxoptimize/.env` →
repo-root `.env`** (specific over general; real env always wins for
CI/prod). Standard dotenv subset: KEY=VALUE, # comments, optional
quotes, no interpolation. `.ctxoptimize/.env` is OURS — for
ctx-optimize-specific URLs (e.g. a read-only replica) that don't belong
in the app's env — and init/up scaffolds `.ctxoptimize/.gitignore`
containing `.env`, so it is ignored BY CONSTRUCTION, no user discipline
required. Repo-root `.env` support means a repo already holding
DATABASE_URL works with zero setup; if that root `.env` is TRACKED in
git, a loud warning fires (an already-exposed secret store should not
be silently built upon). The binary reads these files only while
resolving sources, never copies or prints values. Agents never need to
read any `.env` — the binary resolves names internally, keeping secret
values out of model context entirely. **Literal credentials in a committed entry are a
hard ERROR**: a userinfo password (or credential query param like
`?password=`) that is not a `$VAR` reference refuses with "credentials
belong in env vars". CLI verbs take names/templates too — never a raw
URL with secrets on argv, so nothing secret reaches config, shell
history, or `ps`. Any referenced var unset → the source is SKIPPED
naming the var. The sanitized entry is the source identity (per-source
producer), so re-capturing one DB replaces only its own nodes.

**Routing** (deterministic, applied to the expanded value):

| value shape | connector |
|---|---|
| `postgres://` `mysql://` `mongodb://` `redis://` `kafka://` `nats://` `s3://` | by scheme |
| `http://` / `https://` | openapi (fetch; must parse as a spec, else loud error) |
| no scheme → filesystem path (relative from repo root) | openapi from disk |
| anything else | hard error listing supported schemes |

**Credentials/certs ride each ecosystem's OWN documented URL form**:
`postgres://user:pass@host/db?sslmode=verify-full&sslrootcert=/p/ca.pem&sslcert=…&sslkey=…`
(libpq URI params) · mongodb `?tls=true&tlsCAFile=…&tlsCertificateKeyFile=…`
(official URI options) · `rediss://` + `?cacert=…&cert=…&key=…` ·
`kafka://user:pass@b1:9092,b2:9092?sasl=plain&tls_ca=…&tls_cert=…&tls_key=…` ·
`nats://user:pass@host` or `nats://token@host` (+`?tls_ca=…`) ·
s3 userinfo = access/secret key, endpoint in host; ABSENT userinfo falls
back to the standard AWS credential chain (env/profile/IAM role) ·
https openapi userinfo → Basic/Bearer. Cert PATHS are not secrets and
may sit in the URL; cert CONTENTS never leave the producer's disk, and
the sanitizer strips key-bearing params from stored ids. Exotic auth
(vault-minted certs, custom headers, HSM) = the adapter-script lane.

## Scaffold additions (init/up, same never-overwrite-user-edits rules)

- `.ctxoptimize/.gitignore` containing `.env` — the secret file is
  ignored BY CONSTRUCTION (see resolution ladder above).
- `.ctxoptimize/instructions.md` — a committed, self-contained usage
  card generated from the embedded skill: intent table
  (query/card/change-plan/affected/path), verify discipline, tool-choice
  ladder, sources + adapter help, remote push/pull, up as front door.
  Teammates' agents inherit full usage with ZERO installation on any
  agent CLI; the CLAUDE.md/AGENTS.md pointer blocks shrink to "store
  present — read .ctxoptimize/instructions.md". When a newer binary's
  embedded content differs, `up` refreshes the file (exact-replace, same
  semantics as install --skills) — one person upgrades + commits, the
  whole team's agents upgrade.

## Verbs

- `ctx-optimize add <ENV_NAME>` — resolve, dial, capture, merge, AND
  record the name in config `sources` (idempotent). One command from
  zero to "refreshed on every up". `up`/`add` re-capture all recorded
  sources; unset var → one-line SKIP, not fatal (teammates without
  credentials still `up` cleanly and get these nodes via remote pull);
  `--strict` turns skips into failures (CI). Sources capture in parallel
  (goroutine per source); summary reports per-source outcomes
  (`11 captured, 1 skipped (EVENTS_KAFKA_URL not set)`).
- `ctx-optimize capture <ENV_NAME>` — one connector, Batch on stdout, no
  store write. The composition/debug primitive.
- `ctx-optimize adapter list` — supported schemes + this repo's recorded
  sources + armed custom adapter scripts.
- `ctx-optimize adapter help <scheme>` — the complete setup card: value
  format, credential + cert/tls params, an export example, and the
  paste-ready `add` command — printed from the connector's own declared
  parameter table (never drifts from code). The bundled skill routes
  "get a database/bucket/queue/API into the store" through this help,
  so agents self-serve the flow.

## Capture is native Go

Pure-Go connectors compiled in (no cgo): postgres (pgx), mysql
(go-sql-driver), mongo (mongo-driver), redis (go-redis), s3/minio
(minio-go), nats (nats.go), kafka (franz-go), openapi (fetch/read +
parse). Node shapes: db → schema → table → column (+ FK edges);
bucket → prefix; stream/topic → subject/partition + consumers;
api → path → operation → component schema (+ uses edges). All ride the
existing Batch door as non-file sources (`postgres://host/db/table`) —
`verify` already treats those as store-existence-only. Sources join the
SLOW lane with adapter scripts; the code-only fast lane never dials.

## Cloud/dynamic lane = the existing custom-adapter door

No new protocol. A dropped script opens the tunnel / reads the vault,
sets an env var IN ITS OWN PROCESS, and calls `capture` back by name;
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

Custom adapters (`.ctxoptimize/adapters/*.js|py|sh` emitting a Batch)
remain the escape hatch for source types with no connector.

## Secret hygiene (by construction, then proven)

1. ONE choke point: node ids/sources are written from the SANITIZED URL
   (userinfo + credential query params stripped) before anything touches
   disk — store, wiki, dashboard, and pushed store repos are secret-free
   structurally.
2. Env-names-only enforcement (above) keeps literals out of config/argv.
3. All output redacts: errors, per-source summaries, --json, status,
   audit log print `postgres://user:***@host/db` at most; failure text
   names scheme + host, never userinfo. The resolved URL exists in
   memory for the dial only.
4. Merge gate: a hermetic test plants a distinctive fake password in a
   source URL, runs capture + failing capture + up + status + audit, then
   greps the ENTIRE store tree and all captured stdout/stderr — one hit
   anywhere fails the build. Permanent test; no future connector can
   regress it.

## Doctrine amendment (CLAUDE.md + VISION in this change)

"No DB drivers — ever" becomes: **no LLM, no background network, no
credentials at rest.** Connectors are as deterministic as the code
extractor; they dial ONLY inside `add`/`up`/`capture` at the user's
command (same doctrine as `update` and the zig download).

## Rejected alternatives (this change's history)

- JS templates shelling to psql/mc/nats/kcat: needs node + per-ecosystem
  CLIs on every gathering machine; slower; authoring still user-visible.
- `{"type": …, "env": {…}}` maps: invented key names per connector,
  merge semantics, help tables — all dissolved by the URL convention.
- Script lifecycle protocol (setup/resolve/teardown exports): replaced by
  ordinary adapter scripts calling `capture` with `try/finally`.
- `$(cmd)` in config values: committed command execution surface; the
  adapter-callback lane covers dynamic resolution instead.

## Non-goals

- No connection pooling, retry ladders, or persistent connections — one
  source, one dial, one capture, deterministic.
- No auto-discovery of databases; only recorded/named sources are dialed.
- No secret persistence anywhere, including `adapter add`-style scaffolds.

## Merge gates

- `task ci` + `task golden` green; floors and bench gates untouched — a
  repo with no sources shows ZERO added cost on the gather path.
- Hermetic tests per connector against in-process fakes (httptest for
  openapi/s3; wire-level fakes where feasible); env-gated real-infra
  smokes (`CTX_OPTIMIZE_TEST_PG`, `_MINIO`, `_NATS`) before release for
  postgres, minio, nats, openapi at minimum.
- Behavior tests: `add NAME` records + captures; template expansion
  (partial `$VAR`s) + unset-var skip vs `--strict`; literal-credential
  hard error (userinfo and query-param forms); scheme routing incl.
  no-scheme file path; adapter-callback round-trip; per-source producer
  replace; the secret-grep gate (above).
- Binary size delta recorded in CHANGELOG; skill surfaces (SKILL.md,
  references/adapters.md, activation-routing.xml) updated in the same
  change (house rule).

## Hardening round — spikes + red team, 2026-07-17 (overrides above where in conflict)

Four-agent panel: three compiled spikes + one adversarial review.
Artifacts under the session scratchpad `spike-deps/`, `spike-url/`,
`spike-env/`. Measured facts and the fixes adopted:

### Measured (spikes)

- **Binary delta +12.2 MB (darwin/arm64) / +12.8 MB (linux/amd64)** for
  all 7 drivers, CGO_ENABLED=0 -trimpath -s -w; all pure Go incl. cross
  compile; 58 modules in go.sum. Acceptable next to the 32MB wasm; goes
  in the CHANGELOG.
- **Driver parse errors mostly self-redact** (pgx → `user:xxxxx@`,
  mongo/redis omit the URI) but **`net/url.Parse` errors echo the FULL
  URL including the password** — any surfaced `*url.Error` is a leak.
- **`net/url.Parse` hard-fails on real AWS secrets** (`/` in the secret
  → "invalid port" error) and mishandles kafka/mongo multi-host
  authorities. The sanitizer can NEVER be built on url.Parse.
- **dotenv subset validated** (16-case table green): split on first `=`,
  optional quotes, `export ` prefix, CRLF+BOM tolerated, unquoted `#`
  starts a comment only when preceded by whitespace (bash /
  docker-compose / python-dotenv majority rule).
- **gitignore trap confirmed with real git**: a file committed BEFORE the
  scaffolded ignore stays tracked, and `git check-ignore` exits 1 for it
  (index wins) — detection must be `git ls-files --error-unmatch -- <p>`
  (exit 0 = tracked → warn "git rm --cached"). Same command detects a
  tracked root `.env`; exit 1/128/exec-error = silent no-op.

### Adopted fixes (red team C/H/M/L)

1. **Two-choke secret model (C1).** Choke A (ids): a FAIL-CLOSED textual
   sanitizer — pre-split userinfo at the last `@` in the authority (with
   a defensive last-`@`-before-query tier), strip credential-class query
   params, never url.Parse; if an entry defies parsing, emit the env-var
   NAME only. Choke B (output): the binary knows every resolved secret
   VALUE at dial time — one `scrub(values, text)` wraps ALL connector
   errors, summaries, panics, and stdout/stderr before emission,
   literal-replacing each value. The grep gate gains wrong-password and
   panicking-connector cases per scheme.
2. **Parallel dial, serial merge (C2).** Goroutines dial and build
   batches; ONE writer merges into the store sequentially (store
   Replace/Merge are read-modify-rewrite and racy by design).
3. **Producer identity + reconcile (H1).** Identity = the env-var NAME
   for bare/`$` entries; sanitized template string for templates. `up`
   reconciles: source-namespace producers no longer declared in config
   are reported (and pruned with confirmation) — renames/edits cannot
   permanently orphan ghost schemas.
4. **argv is names-only (H2/H3).** `add`/`capture` accept ONLY
   `^[A-Z_][A-Z0-9_]*$` env-var names on argv; templates live in config.
   This kills shell pre-expansion leaks AND disambiguates from the
   existing `add [dir]` gather form (var-name shape = source, else
   path). The catalog rides the EXISTING `adapters` verb —
   `adapters list|help <scheme>` — no near-duplicate `adapter` verb.
5. **Three outcomes + freshness (H4/H5).** Per-source outcome =
   captured | skipped(unset) | failed. `add` records config ONLY after a
   successful capture. Failed keeps prior nodes, reports loudly, exits
   non-zero only under `--strict`. The store's freshness file records
   sanitized id + last-captured ts; `up`/`status` print staleness
   (`skipped — nodes last captured 180d ago`).
6. **Sources TTL (M3).** `up` re-dials a source only when its freshness
   is older than 24h (default) — `--sources=always|never` overrides;
   `add`/`capture` always dial. Agents running `up` reflexively must not
   schema-scan 12 prod DBs dozens of times a day.
7. **s3 disambiguation (M1).** No userinfo AND host without dot/port ⇒
   AWS convention (`s3://bucket/prefix`, cred chain); userinfo form
   requires a dotted or ported endpoint host; ambiguous ⇒ hard error
   naming both forms.
8. **https hygiene (M2/M6).** Stored ids for http(s) sources strip ALL
   query params (allowlist-in, not denylist-out — `?token=`/`?sig=`
   vocab is unbounded). Routing errors for non-spec URLs point to the
   adapter-script lane. OpenAPI ingestion drops security-scheme
   example/secret values; other spec content is stored as-is (informed
   decision, in the docs).
9. **Resolution transparency (M4).** Per-source summary names the
   origin: `BILLING_DB_URL ← .ctxoptimize/.env` (names only). The
   scaffolded `.ctxoptimize/.gitignore` covers `.env*` with
   `!.env.example`.
10. **instructions.md managed block (M5).** Markered block with an
    embedded version stamp; refresh rewrites only the block and only
    UPGRADES (an older binary never downgrades a newer committed file) —
    no clobbered team edits, no commit ping-pong.
11. **Ergonomics (spike-url, L3).** Literal USERNAMES allowed (only
    literal passwords/secret query values hard-error); single-letter
    "scheme" (`C:\…`) routes as a file path; unsupported-shape errors
    describe what was detected; `adapters help` + dial-failure hints
    document percent-encoding (`/` → `%2F`) for secrets with URL-special
    chars; multi-host authorities pass to drivers verbatim (never
    Hostname()/Port() on them for ids).
12. **Catalog + accepted risks (L1/L2).** mssql (pure-Go go-mssqldb)
    joins the catalog for the enterprise claim — 9 connectors; oracle
    stays out (cgo). The child-process `/proc/environ` exposure in the
    adapter-callback lane is same-user-only ambient risk: accepted,
    documented.

## Single binary confirmed — the init-cost autopsy (2026-07-17)

Directed: "check if it's helpful or single binary is enough." Measured
chain (30-run medians, warm cache, darwin/arm64):

1. hello-world 2.4ms vs +7 drivers 22.1ms — looked like "drivers cost
   20ms/invocation" and briefly justified a companion binary.
2. `GODEBUG=inittrace=1` autopsy: **github.com/rs/xid — a transitive dep
   of minio-go — burns 15.0ms in ONE init**; the other 162 package inits
   across all remaining drivers total **0.6ms**.
3. THE REAL BINARY, real store, real `query`: plain 5.49ms vs
   six-drivers-linked (pgx, mysql, mongo, redis, nats, kafka) 5.98ms —
   **+0.49ms**, under +3% of the golden query benchmark (~19ms), well
   inside the ≤+10% gate.

Decision: **ONE binary. minio-go is BANNED as a dependency.** The s3
connector is a small stdlib client instead — SigV4 signing +
ListBuckets/ListObjectsV2 XML (~200 lines; listing only, which is all
capture needs). No companion binary (that draft is superseded — it was
solving a problem one transitive dependency caused).

Permanent gates so this never regresses silently:
- startup benchmark: main-binary bare invocation p50 within +1ms of the
  previous release;
- init budget test: `GODEBUG=inittrace=1` total across all package
  inits must stay < 2ms — any future dependency bringing an expensive
  init fails CI with the package named.

## The logical-shape rule — EVERY connector (directed 2026-07-17:
"timescale/partitions … that's the style for other external systems
as well")

Universal principle: capture the LOGICAL shape a developer reasons
about; never enumerate physical/instance data; every capture is
bounded-work with deterministic output. Per engine:

- **postgres**: skip `pg_*`, `information_schema`, `_timescaledb_*`;
  partitioned PARENT = one node with `partitions: N`
  (pg_partitioned_table/pg_inherits); chunks/children never enumerated.
- **mysql/mssql**: system schemas skipped (`mysql`, `sys`,
  `performance_schema`, `information_schema` / `sys`,
  `INFORMATION_SCHEMA`); mysql partitions are metadata-only (count fact).
- **mongo**: skip `admin`/`local`/`config` dbs and `system.*`
  collections; field names from a CAPPED sample (~100 docs), sorted.
- **redis**: NEVER a full keyspace walk — bounded SCAN sample,
  summarized by key-prefix pattern (`billing:*` → one node with approx
  count + example type), hard cap on sampled keys.
- **kafka**: skip internal topics (`__consumer_offsets`,
  `__transaction_state`, `_schemas`, leading `__`/`_` internals); topic
  node carries `partitions: N` as a fact — partitions are not nodes;
  consumer groups listed by name only.
- **nats**: skip `$SYS.*`; stream node carries subject list + consumer
  names; no per-message anything.
- **s3**: ListObjectsV2 with `delimiter=/` — PREFIXES only, depth
  capped (2 levels), result count capped; objects are never enumerated
  as nodes; prefix nodes carry approximate counts.
- **openapi**: specs are already logical; security-scheme example/secret
  values stripped (M6).

Any cap that truncates is REPORTED in the summary line ("s3: 2 levels,
capped at 500 prefixes") — silent truncation reads as full coverage.
No external tools assumed on the user's machine — pg_dump/tbls/atlas
appear in benchmarks as comparison baselines only; capture is wire-
protocol-native from the binary.

## Measured: postgres capture speed vs competitors (2026-07-17, Stage 0)

Corpus: postgres:16, 100 tables / 3 schemas / 1,307 cols / 139 FKs /
200 indexes / 10 views + THE TRAP (1 table with 100 partitions +
500 fake TimescaleDB chunks + catalog = 706 raw tables). 5-run medians,
connect included, localhost:

| tool | median | result quality |
|---|---|---|
| **ours (pgx, filtered)** | **31 ms** | **101 logical tables**, partitions:100 as fact, full cols/PK/FK/idx/views/comments |
| ours naive (no filter) | 33.8 ms | 706 junk tables — filtering is FREE (faster, even) |
| pg_dump --schema-only | 101 ms | 706 tables, 478 KB DDL noise |
| atlas schema inspect | 248 ms | 606 tables (collapses children, still emits 500 chunks) |
| tbls | 1,356 ms | 716 mixed junk |

Claim (CHANGELOG-ready): captures a 100-table/3-schema postgres in
~31 ms incl. connect — 3x pg_dump, 8x atlas, 40x tbls — and where a
100-partition table + 500 Timescale chunks bloat every other tool to
600–716 raw tables, it emits 101 logical tables with partition counts
as facts, at zero added cost. (Localhost; remote adds RTT × ~8 fixed
round trips — few-big-queries design.)

Gates adopted: hermetic-fake capture < 50 ms; env-gated real-pg smoke
introspection < 250 ms (8x margin); correctness pin on the trap corpus:
tables=101, partition_children_collapsed=100, schemas=3 — never 706.
Reference implementation with the exact catalog queries preserved in
the session scratchpad (bench-pg/introspect/main.go) for Stage 2.

## FINAL architecture: companion binary after all — real-store measurement
(2026-07-17, supersedes "Single binary confirmed")

The single-binary verdict was based on blank-import linking (+0.49ms).
With the nine REAL connectors compiled in: binary 43.2→56.4MB stripped,
bare startup 4.00→5.37ms, and **query on the real 2158-node store
11.76→13.31ms = +13% — breaches the ≤+10% query gate**. No single
driver dominates (mssql removal changed nothing); it is constant
process-load cost spread across the driver payload. Per the standing
directive ("should not make performance slower — probably we will ship
a separate cli"), the split is adopted:

- **`ctx-optimize-adapters`** — second binary, same repo
  (cmd/ctx-optimize-adapters), connector files move to
  internal/sources/connectors (driver imports live ONLY there). Shipped
  in the same archives/npm packages, side by side.
- **Main binary: zero driver imports — byte-identical hot paths.** The
  sources CORE (entry/dotenv/scrub/run/registry/verbs) stays in main.
- **Exec bridge, names-only**: for a dial, main execs the sibling
  (`ctx-optimize-adapters capture <NAME>`) found beside its own
  executable; the child re-resolves the SAME env/.env ladder (identical
  result, deterministic), emits Batch JSON on stdout; main merges
  serially. No URL ever crosses argv. `adapters help <scheme>` proxies
  the same way (param tables live only in connector code — no drift).
  Companion missing → loud error naming the binary + install hint.
  In-process registry retained for test stubs.
- Gates: main-binary query p50 within noise of previous release
  (replaces the +1ms startup gate); companion carries the size/init
  budget lines.

## E2E round — all 9 connectors vs live services (2026-07-17)

Docker arena (postgres 16, mysql 8, mongo 7, redis 7, nats 2 -js, kafka
3.7 KRaft, MinIO, azure-sql-edge) + local OpenAPI server; env-gated
driver smokes AND the real-binary lane (`add`/`up --sources always`
through the exec bridge, creds via `.ctxoptimize/.env`): 9/9 captured,
store + repo scrub-clean.

**Bug found and fixed by the lane**: a bare entry name's VALUE never got
a template pass, so the documented folded shape
(`DOCS_S3_URL='s3://$MINIO_KEY:$MINIO_SECRET@host/bucket'`) sent literal
`$MINIO_KEY` to the wire (MinIO 403 InvalidAccessKeyId). Fix: exactly one
extra LENIENT pass over a bare name's resolved value — resolvable refs
substitute, unresolvable $tokens stay literal (provider passwords with
'$' keep working), substituted values still never rescanned. Pinned in
TestExpandBareNameValueGetsLenientTemplatePass.

Note for e2e reruns: azure-sql-edge's self-signed cert has a negative
serial (Go x509 rejects it) — the sqlserver URL needs
`encrypt=disable`; that is container-side, not ours.

## Amendment (2026-07-17, owner-directed): ladder simplified — no .ctxoptimize/.env, no scaffolded gitignore

The `.ctxoptimize/.env` tier and the scaffolded `.ctxoptimize/.gitignore`
are REMOVED. Final ladder: process env → repo-root `.env` →
`~/.config/ctx-optimize/.env` (machine-global, for URLs shared across every
repo on this machine; lives outside any repo so it can never be committed;
`CTX_OPTIMIZE_GLOBAL_ENV` overrides the path for tests). Nothing secret
lives in `.ctxoptimize/` anymore, so no ignore file is scaffolded; the
tracked-root-`.env` loud warning stays.
