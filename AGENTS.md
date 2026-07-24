# ctx-optimize — agent context

Standalone Go CLI + agent skill. **The binary is deterministic: no LLM calls,
no DB drivers, no embeddings, no MCP — ever.** The host agent is the only
intelligence; documents/DB/messaging enter via skill-level adapters through
the validated `add --json` door.

## Layout (crossmemcli house pattern)

- `cmd/ctx-optimize/` — 10-line shim → `internal/app.Run(args, stdout, stderr)`
- `internal/schema` — THE emit contract (Node/Edge/Batch + fail-closed Validate)
- `internal/store` — central store `~/ctxoptimize/<repo-name>/` (key = basename, or config `name`), ndjson graph, content-hash manifest; Merge stamps producer only when absent (merge preserves provenance)
- `internal/project` — repo-level `.ctxoptimize/` dir (committable; the ONLY thing we put in a user's repo): `config.json` (name + remote [string or {type,url,credentials}]) + `adapters/` (dropped scripts discovered by extension: .js/.mjs→node, .py→python3, .sh→sh; `init` scaffolds with inert example.js.sample). `${VAR}` resolves from env at sync time; resolved values never written/printed
- `internal/remote` — sync-only remotes: `file://` + `s3://` (stdlib SigV4, no SDK); push/pull take NO url — remote comes from `.ctxoptimize/config.json` (or store config via `remote init --local`)
- `internal/extract/markdown` — tier-1 doc producer (dup heading slugs get -2/-3 suffixes)
- `internal/extract/code` — tier-1 code producer: tree-sitter grammars compiled to WASI (scripts/wasm/build.sh, zig cc; treesitter.wasm COMMITTED ~19MB, go:embed), wazero host (pure Go, one instance per worker goroutine). Embedded langs: go/py/js/ts/tsx/java/c/cpp/c#/rust/zig/sql (32MB wasm). Other languages = grammar PACKS: <name>.wasm (scripts/wasm/build-grammar.sh) + <name>.json (node-type mapping) in ~/ctxoptimize/grammars/ or repo .ctxoptimize/grammars/ — discovered at add-time, pack exts override embedded, malformed packs fail loudly. kotlin/swift/dart ship as packs in grammars/. `grammar build <dir|github-url>` (internal/grammar) builds packs in pure Go — zig from PATH or auto-downloaded once (sha256-verified vs ziglang.org index) into ~/ctxoptimize/toolchain/, runtime tarball cached, mapping draft auto-suggested from node-types.json (marked _review). No shell scripts in the user path; scripts/wasm/build.sh is dev-only for the embedded bundle. Emits file/decl nodes (qualified labels, L#-L#), contains + imports (EXTRACTED), calls resolved module-wide by unique name (INFERRED; ambiguous dropped). ~0.5s for 4k files.
- `internal/query` — lexical IDF + prefix + trigram tiers + budget; complete hits (S1e: no pointer lists)
- `internal/analyze` — pure graph verbs: path, explain, affected (reverse impact), hubs; Resolve = id > label > fuzzy tokens
- store.Replace = producer-scoped truth on `add` (stale nodes pruned; <50% shrink refused without --force); Merge (--json door) stays upsert
- `internal/dashboard` — `serve|dashboard` verb: embedded single-file UI (go:embed, ZERO external requests — no CDN ever) + read-only JSON API (/api/modules|graph|query); binds 127.0.0.1:4747; read path must never create store dirs
- `internal/skills` — embedded SKILL.md, `install --skills` fans to claude+codex
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

<!-- ctx-optimize:begin -->
<ctx-optimize>
  <precondition>Run `command -v ctx-optimize` first. If it is NOT installed, IGNORE this entire
  block and answer by reading the code normally — the store is an optimization, not a requirement
  (install later with `npm install -g @muthuishere/ctx-optimize`, or download the binary). Everything
  below applies ONLY when the command exists.</precondition>
  <store>Pre-built knowledge store at `~/ctxoptimize/ctx-optimize/` (config in `.ctxoptimize/` here).</store>
  <use>Use it INSTEAD of grep-and-read chains — PICK BY INTENT: find → `ctx-optimize query "<terms>"` ·
  inspect a symbol → `card <symbol>` · about to EDIT → `change-plan <symbol>` (callers+impact+tests, one
  call) · blast radius → `affected <symbol>` · connection → `path <a> <b>` ·
  list/filter (no jq): `nodes --kind K` / `edges --relation R` / `deps`. wiki at
  `~/ctxoptimize/ctx-optimize/wiki/`. Output is parsed fact with exact file:line — cite it directly, do
  NOT re-verify in source; open a file only for a body the store didn't show. Exhaustive literal-string
  sweeps stay grep's job.</use>
  <deep-doc>The FULL usage card — verify discipline, store-vs-grep ladder, sources (databases/
  buckets/queues/APIs by env-var name), remote push/pull, `up` — is committed at
  `.ctxoptimize/instructions.md`. Read it before deeper store work.</deep-doc>
  <no-local-store>Fresh clone with nothing at `~/ctxoptimize/ctx-optimize/`? Run `ctx-optimize up` —
  it pulls the team's prebuilt store when the config declares one, otherwise rebuilds in seconds.</no-local-store>
</ctx-optimize>
<!-- ctx-optimize:end -->
