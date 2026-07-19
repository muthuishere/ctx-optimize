# merge cannot address nested module stores — DRAFT, no code until owner agrees

Date: 2026-07-19 · Status: DRAFT (found by the 32-scenario validation matrix, S25)

## Problem — reproduced

In a multi-module repo (root key `mono`, modules stored at
`mono/services/api`, `mono/services/worker`):

```
$ ctx-optimize merge services/api services/worker --into everything
ctx-optimize: no module "api" in ~/ctxoptimize — run `ctx-optimize add` there first
```

`cmdMerge` (internal/app/app.go:1681) resolves a dir argument via config
name > basename (`store.ModuleKey`) and a bare name via `SanitizeKey`
(flattens "/" → "-"). Neither produces the nested key
(`SanitizeKeyPath(rootKey + "/" + seg)`) that fan-out actually writes —
so declared-module stores are unreachable by merge. Every OTHER verb
resolves scope correctly; merge predates the multi-module layout.

## Proposed fix

Dir arguments resolve through the SAME scope resolution as every verb
(`resolveScope` on the dir → its storeKey), falling back to today's
name/basename lane for standalone dirs. Bare-name arguments additionally
try the store-relative path form (`mono/services/api`) before flattening.
Gate: scenario S25 flips from known-limitation to verified; single-module
merge byte-identical.

## Until then

Documented limitation in docs/monorepos.md: merge addresses top-level
store keys only; nested module stores of a declared monorepo cannot be
merged.
