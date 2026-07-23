# Spikes — deplink resolution measured on real stores (2026-07-23)

Method: the proposed resolution rules implemented as a ~60-line node script
run against real exported/on-disk stores — no product code touched. Scripts
kept in the session scratchpad; numbers below are the pinned facts.

## Spike 1 — this repo (mixed go + npm), exported graph

Corpus: `ctx-optimize export --format json` of the ctx-optimize store —
2,956 nodes, 5,632 edges, 128 `module://` nodes, 43 `dep:` nodes
(32 go, 10 npm), 1,112 imports edges.

| metric | value |
|---|---|
| links found | **22** (npm exact + go longest-prefix; incl. subpath cases like `nats.go/jetstream` → `dep:go/…/nats.go`) |
| skipped correctly | 47 go stdlib · 5 node builtins · 22 relative |
| unresolved | 32 — ALL our own module's internal packages (`github.com/muthuishere/ctx-optimize/internal/…`) |
| link wall time | **0.286 ms** |
| edge growth | **+0.39%** |

Findings folded into the ADR:
1. **Self-module rule (new)**: go unresolved = the repo's own import paths.
   The linker must read `module` from go.mod and skip that prefix — they are
   internal, already covered by internal imports edges.
2. **Language partition (impl note)**: resolution should pick the ecosystem
   from the importing file's language, not try npm-then-go on every
   specifier (worked here only because the namespaces don't collide).

## Spike 2 — scale: mastra-main (242-module TS monorepo store)

Corpus: every per-module store under `~/ctxoptimize/mastra-main/` —
160,836 nodes, 220,566 edges, 11,238 `module://` nodes, 3,636 npm `dep:`
nodes. Linking run per-module, as the real linker would inside `gatherInto`.

| metric | value |
|---|---|
| links found | **3,632** — 84% of the 4,336 external candidates |
| skipped (relative/builtin) | 6,902 |
| unresolved | 704 — dominated by cross-workspace `@mastra/*` imports not declared in the importing module's own package.json |
| link wall time, ALL 242 modules | **1.4 ms total** (~6 µs/module; spike's 307 ms ndjson read/parse is not paid by the real linker — batches are already in memory) |
| edge growth | **+1.65%** (3,632 on 220,566) |

Findings:
3. **Perf claim confirmed at 50× scale**: linking is hash lookups; even the
   largest local store costs ~1 ms end-to-end. The ADR's "microseconds per
   module" is measured, not estimated.
4. **Workspace-internal imports (open question 5)**: the 704 unresolved are
   real signal — `@workspace/pkg` imported but not declared in that module's
   manifest. Options: resolve against the workspace-member set (npm
   workspaces edges already exist: `depends_on` via `expandWorkspaces`),
   emit nothing (draft), or surface as a lint-grade fact later. Worth an
   owner call — it is the monorepo-shaped version of the reporter's
   "undeclared integration" drift signal.

## Conclusion

Both asks of issue #5 are satisfiable at negligible cost: zero new nodes,
edge growth 0.4–1.7%, link time ≤1.4 ms on a 220k-edge monorepo. Link
precision is exact for npm/go by construction; recall on externals measured
at 84–100% with all misses explained (self-module, workspace-internal).
