# ctx-optimize — agent context

Standalone Go CLI + agent skill. **The binary is deterministic: no LLM calls,
no DB drivers, no embeddings, no MCP — ever.** The host agent is the only
intelligence; documents/DB/messaging enter via skill-level adapters through
the validated `add --json` door.

## Layout (crossmemcli house pattern)

- `cmd/ctx-optimize/` — 10-line shim → `internal/app.Run(args, stdout, stderr)`
- `internal/schema` — THE emit contract (Node/Edge/Batch + fail-closed Validate)
- `internal/store` — central store `~/ctxoptimize/<repo-name>/` (key = basename, or ctx-optimize.json `name`), ndjson graph, content-hash manifest
- `internal/project` — repo-level `ctx-optimize.json` (committable: name + remote [string or {type,url,credentials}] + declared adapters; the ONLY file we put in a user's repo). `${VAR}` resolves from env at sync time; resolved values never written/printed
- `internal/remote` — sync-only remotes: `file://` + `s3://` (stdlib SigV4, no SDK); push/pull take NO url — remote comes from ctx-optimize.json (or store config via `remote init --local`)
- `internal/extract/markdown` — tier-1 producer (code langs via tree-sitter WASM: next)
- `internal/query` — lexical IDF + prefix tier + budget; complete hits (S1e: no pointer lists)
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
