# ctx-optimize — agent context

Standalone Go CLI + agent skill. **The binary is deterministic: no LLM calls,
no embeddings, no MCP, no credentials at rest — ever. The MAIN binary keeps
no DB drivers; the `ctx-optimize-adapters` companion carries them, dialing
ONLY inside `add`/`up`/`capture` at the user's command** (same doctrine as
`update` and the zig download). The host agent is the only intelligence;
DB/bucket/queue/API schemas enter via native sources (env-var name → URL →
connector), everything else via adapter scripts through the validated
`add --json` door.

## Layout (crossmemcli house pattern)

- `cmd/ctx-optimize/` — 10-line shim → `internal/app.Run(args, stdout, stderr)`
- `cmd/ctx-optimize-adapters/` — companion shim → `internal/adapterscli.Run`; the ONLY binary with driver imports (main stays 43.2MB/query-noise-neutral), shipped beside main in every archive/npm pkg; main execs it via the bridge (internal/sources/bridge.go, names-only argv, child re-resolves the env ladder; missing → loud error + install hint), `adapters help` proxies param tables from it (no drift)
- `internal/sources` — native sources core (ADR 2026-07-17): entry (expand $VAR, DetectLiteralCreds hard-errors literal passwords, Route by scheme, fail-closed textual Sanitize — never net/url.Parse), dotenv ladder (process env → root .env → ~/.config/ctx-optimize/.env machine-global, CTX_OPTIMIZE_GLOBAL_ENV override for tests; tracked-.env warning), scrub (value choke on ALL output), run (parallel dial → SERIAL merge; outcomes captured|skipped|failed; 24h TTL; per-source freshness stamps in <store>/sources.json; Reconcile prunes undeclared producers), registry (Connector iface + HelpCard from Params()). Producer id = `source:<var-or-sanitized-template>`
- `internal/sources/connectors` — the 9 connectors (postgres/mysql/mongo/redis/kafka/nats/s3/mssql/openapi; s3 = stdlib SigV4, minio-go BANNED — 15ms init), driver imports live ONLY here; logical-shape rule: system schemas skipped, partitions collapsed to parent `partitions:N`, bounded samples, caps reported (trap pin: 101 tables, never 706; 31ms real-pg capture)
- `internal/adapterscli` — companion dispatcher: capture/help/schemes over the in-process registry
- `internal/schema` — THE emit contract (Node/Edge/Batch + fail-closed Validate)
- `internal/store` — central store `~/ctxoptimize/<repo-name>/` (key = basename, or config `name`), ndjson graph, content-hash manifest; Merge stamps producer only when absent (merge preserves provenance)
- `internal/project` — repo-level `.ctxoptimize/` dir (committable; the ONLY thing we put in a user's repo): `config.json` (name + remote {push,pull} commands + sources[] — literal-cred gate at Load) + `adapters/` (dropped scripts discovered by extension: .js/.mjs→node, .py→python3, .sh→sh) + transport samples + `instructions.md` (the committed usage card, version-stamped managed block, refreshed by init/up UPGRADE-ONLY, user text outside markers untouched; CLAUDE.md/AGENTS.md pointer blocks reference it as the deep doc); `init` scaffolds all of it inert (example.js.sample, push/pull.js.sample, remote.example.md). Secrets stay env-var NAMES; the shell expands them at run time
- remote = YOUR script (v0.4, ADR 2026-07-16-scripted-remote-transports): the binary ships NO transport — `remote push|pull` run the commands declared in `.ctxoptimize/config.json` (`{"remote": {"push": "<cmd>", "pull": "<cmd>"}}`) with CTX_STORE_DIR/CTX_STORE_KEY/CTX_SCOPE_PREFIX/CTX_DIRECTION in env; init scaffolds an inert git-lane sample pair; legacy v0.3 URL configs load inert
- `internal/extract/markdown` — tier-1 doc producer (dup heading slugs get -2/-3 suffixes)
- `internal/extract/code` — tier-1 code producer: tree-sitter grammars compiled to WASI (scripts/wasm/build.sh, zig cc; treesitter.wasm COMMITTED ~19MB, go:embed), wazero host (pure Go, one instance per worker goroutine). Embedded langs: go/py/js/ts/tsx/java/c/cpp/c#/rust/zig/sql (32MB wasm). Other languages = grammar PACKS: <name>.wasm (scripts/wasm/build-grammar.sh) + <name>.json (node-type mapping) in ~/ctxoptimize/grammars/ or repo .ctxoptimize/grammars/ — discovered at add-time, pack exts override embedded, malformed packs fail loudly. kotlin/swift/dart ship as packs in grammars/. `grammar build <dir|github-url>` (internal/grammar) builds packs in pure Go — zig from PATH or auto-downloaded once (sha256-verified vs ziglang.org index) into ~/ctxoptimize/toolchain/, runtime tarball cached, mapping draft auto-suggested from node-types.json (marked _review). No shell scripts in the user path; scripts/wasm/build.sh is dev-only for the embedded bundle. Emits file/decl nodes (qualified labels, L#-L#), contains + imports (EXTRACTED), calls resolved module-wide by unique name (INFERRED; ambiguous dropped). ~0.5s for 4k files.
- `internal/query` — lexical IDF + prefix + trigram tiers + budget; complete hits (S1e: no pointer lists)
- `internal/analyze` — pure graph verbs: path, explain, affected (reverse impact), hubs; Resolve = id > label > fuzzy tokens
- store.Replace = producer-scoped truth on `add` (stale nodes pruned; <50% shrink refused without --force); Merge (--json door) stays upsert
- `internal/dashboard` — `serve|dashboard` verb: embedded React app (dashboard-ui/ Vite+React+TS, BUILT dist committed at internal/dashboard/ui/ via go:embed — treesitter.wasm precedent, `task ci`/`go install` never need node; ZERO external requests — no CDN ever, CSP-tested). Reads: /api/modules|graph(budgeted, ?center= expand)|query|usage|stores|setup|audit — read path must never create store dirs. Mutations (onboard scan/confirm, repo re-gather, config set, store delete, remote push/pull): loopback-only by RemoteAddr even if --host widened + per-process X-Ctx-Token (GET /api/token, loopback-only) + routed through the SAME cmd funcs the CLI dispatches (Ops closures from internal/app) + every one audited. Dev loop: `task dashboard-dev|dashboard-build`. Binds 127.0.0.1:4747
- `internal/audit` — append-only `<store-root>/audit.ndjson` (ts, actor dashboard|cli, action, target, before/after sha256; sorted fields, git-diffable); dashboard mutations AND `config` set write it; `log` verb prints it (--json)
- `internal/skills` — embedded SKILL.md, `install --skills` fans to claude+codex
- `internal/selfupdate` — `update`'s binary lane: npm channel delegates to `npm install -g`, goreleaser standalone downloads from GitHub Releases (sha256 vs checksums.txt, atomic swap), dev builds left alone. User-invoked network ONLY (same doctrine as grammar's zig download) — never a background check
- `npm/` — optionalDependencies wrapper (5 platform pkgs, thin launcher, no postinstall)

## Rules

- The gate: `task ci` (vet + test + build + smoke). Green before any commit.
- Tests: stdlib only, beside the code, hermetic (`t.TempDir` + `CTX_OPTIMIZE_STORE`);
  integration tests are env-gated runtime skips (`CTX_OPTIMIZE_TEST_S3`), never build tags.
- Plain files only; store artifacts must stay git-diffable (sorted output, atomic rename).
- Secrets by env-var NAME at call time; never store, print, or log values.
- Spec-driven: decisions live in `openspec/` + `docs/VISION.md` (+ `docs/CRITIQUE.md`
  as the standing counter-weight). Every claim traces to a measured spike.
- Conventional commits; never add Co-authored-by.
- Golden net: `internal/golden/` pins extraction snapshots, landmark facts,
  perf ceilings, AND a judged 20-question scoreboard per corpus (floors in
  `testdata/questions/*.json`, marks in CHANGELOG). Any session touching
  extract/query/analyze MUST run `task golden` (hermetic) and — for
  extraction-surface changes — the corpus tier against pinned clones
  (`git clone --depth 1 --branch v6.9 …linux`, `--branch 13.0.3
  …Newtonsoft.Json`; `CTX_OPTIMIZE_GOLDEN_CORPORA=<dir> task golden`).
  Score and speed may only move UP; raising a floor is a reviewed diff.

<!-- ctx-optimize:begin -->
<ctx-optimize>
  <precondition>Run `command -v ctx-optimize` first. If it is NOT installed, IGNORE this entire
  block and answer by reading the code normally — the store is an optimization, not a requirement
  (install later with `npm install -g @muthuishere/ctx-optimize`, or download the binary). Everything
  below applies ONLY when the command exists.</precondition>
  <store>Pre-built knowledge store at `~/ctxoptimize/ctx-optimize/` (config in `.ctxoptimize/` here).</store>
  <use>For questions about this codebase — where is X, how does Y work, who calls Z, what breaks if I change W —
  use it INSTEAD of grep-and-read chains, not in addition:
  PICK BY INTENT — find: `ctx-optimize query "<terms>"` · inspect a symbol: `ctx-optimize card <symbol>` ·
  about to EDIT: `ctx-optimize change-plan <symbol>` (callers+impact+tests, one call) · blast radius:
  `ctx-optimize affected <symbol>` · `ctx-optimize path <a> <b>` · wiki at `~/ctxoptimize/ctx-optimize/wiki/`.
  Output is parsed fact with exact file:line — cite it directly, do NOT re-verify in source; open a file only
  when the answer needs a body the store didn't show. Exhaustive text sweeps (every literal occurrence of a
  string) are still grep's job.</use>
  <no-local-store>Fresh clone with nothing at `~/ctxoptimize/ctx-optimize/`? Run `ctx-optimize up` —
  it pulls the team's prebuilt store when the config declares one, otherwise rebuilds in seconds.</no-local-store>
</ctx-optimize>
<!-- ctx-optimize:end -->
