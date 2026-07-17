# Tasks — native sources (env-var URL convention)

Staged; each stage lands only with `task ci` green. Golden + bench gates
before merge; capture-speed benchmark vs competitors recorded here.

## Stage 0 — measure (parallel with stage 1)
- [x] Postgres capture-speed benchmark: OURS 31ms filtered (101 logical
      tables from a 706-raw-table trap corpus) vs pg_dump 101ms /
      atlas 248ms / tbls 1356ms; gates: hermetic <50ms, real-pg smoke
      <250ms, trap pin tables=101.

## Stage 1 — core plumbing (internal/sources + app wiring)
- [x] entry parsing: expand ($VAR/${VAR}, single-pass), detectLiteralCreds
      (raw entry; literal passwords/secret query values error, usernames ok),
      route (:// keyed, lowercase; no-scheme→file; single-letter→file),
      sanitize (textual, fail-closed → env NAME only)
- [x] scrub(values, text) output choke; wraps ALL source errors/summaries
- [x] dotenv ladder: process env → .ctxoptimize/.env → root .env
      (validated subset rules); origin reported name-only; tracked-.env
      warning via git ls-files --error-unmatch
- [x] config `sources` array; literal-cred hard error on load
- [x] verbs: add <NAME> (var-name regex disambiguates from add [dir];
      capture then record-on-success), capture <NAME>, adapters list|help
- [x] up/add: parallel dial goroutines → SERIAL merge; outcomes
      captured|skipped|failed; --strict; 24h TTL (--sources=always|never)
- [x] freshness per source (sanitized id + ts); staleness in status/up;
      producer identity = var name (templates: sanitized entry); up
      reconciles undeclared source producers (report; prune on confirm)
- [x] scaffold: .ctxoptimize/.gitignore (`.env*` + `!.env.example`),
      instructions.md managed block (version-stamped, upgrade-only)

## Stage 2 — connectors (internal/sources/<type>.go, parallel agents)
- [x] postgres (pgx): schemas→tables→columns+FK edges; ?schemas= filter;
      skip pg_*/information_schema/_timescaledb_*; collapse partitions to
      parent node with partitions:N (pg_partitioned_table/pg_inherits)
- [x] openapi (stdlib): URL/file, 3.x + swagger 2.0; strip security
      example values; ids strip ALL query params
- [x] s3 (STDLIB ONLY — minio-go banned): SigV4 + ListBuckets/
      ListObjectsV2; AWS-convention disambiguation
- [x] mysql, mongo, redis, kafka (franz-go), nats, mssql
- [x] multi-host authorities passed to drivers verbatim

## Stage 3 — gates + surfaces
- [x] hermetic connector tests (httptest/wire fakes); secret-grep gate
      (fake password planted; store tree + all output greped; incl.
      wrong-password + panicking connector)
- [x] init budget test (inittrace total < 2ms) + startup p50 gate (+1ms)
- [x] task ci + task golden green; bench gates unchanged
- [x] skill: SKILL.md, references/adapters.md, activation-routing.xml,
      hook line; instructions.md content
- [x] docs: README (root+npm), VISION, CHANGELOG (+size delta), doctrine
      note (main binary keeps no-drivers? superseded — drivers in main
      binary, minio-go banned; amend CLAUDE.md)
- [x] env-gated real-infra smokes: CTX_OPTIMIZE_TEST_PG/_MINIO/_NATS
