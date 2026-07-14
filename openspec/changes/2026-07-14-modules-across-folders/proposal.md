# ADR — modules are manifest-defined path SETS, not folder subtrees; test projects are edges, not boundaries

Status: ACCEPTED 2026-07-14 (maintainer). **Move 1 is the build** (module =
name + path set, in the deterministic binary). **Move 2 is REASSIGNED to the
agent skill** — the binary does NOT parse `.sln`/`settings.gradle`; the host
agent reads the solution manifest and writes `.ctxoptimize/config.json`
`modules[]`, keeping the binary deterministic (no manifest-derived module
guessing in core). **Move 3** (test-coupling `tested_by` edges) stays as a
follow-on. This changed a load-bearing assumption (module == directory subtree).

## The two problems (maintainer, 2026-07-14)

1. **C#: source is one project, test is a different project.** `Billing.csproj`
   and `Billing.Tests.csproj` are separate compilation units, conventionally in
   separate top-level folders (`src/Billing/`, `tests/Billing.Tests/`).
2. **A module may be split across folders, not grouped under one directory.**
   A logical module (a .NET solution, a Gradle multi-project, a Bazel package
   set) is a *manifest-enumerated set of scattered paths*, not a subtree.

Our current model breaks on both: `scan` treats "a directory with a build
marker" as a module and mirrors the folder layout into one store per subtree
(`internal/scan`, `internal/app/multimodule.go`). That is correct for
genuinely independent deployables (separate `go.mod` services) but wrong when
the build system, not the filesystem, defines the module.

## How the established tools handle it (research, 2026-07-14)

- **The .NET convention IS split-across-folders.** The canonical layout
  (davidfowl's .NET structure gist; Microsoft docs; JetBrains Rider) is `src/`
  for source projects and `tests/`/`test/` for test projects — different
  top-level folders. A `.sln` enumerates project paths that live *anywhere*,
  and *solution folders* are virtual groupings independent of disk layout
  (`dotnet sln add --solution-folder`). So a solution = a named set of
  scattered `.csproj` paths. `ProjectReference` crosses directories freely
  (`ProjectB` → `../ProjectA/ProjectA.csproj`). Same shape in Gradle
  (`settings.gradle` `include ':app', ':lib'` → arbitrary `projectDir`), Bazel
  (BUILD targets), Nx/Turbo (`project.json`/workspace globs).
- **Boundaries are QUERYABLE, not stored partitions.** CodeGraph, graphify,
  GitNexus index a repo as ONE graph (one SQLite/embedded DB) and let the
  graph's edges + node metadata express module structure; you *query* a module,
  you don't gather it into a separate physical store. Madge / dependency-cruiser
  enforce boundaries but don't partition storage. (falkordb code-graph;
  harness.io "your repo is a knowledge graph"; colbymchenry/codegraph.)
- **Test coupling is a first-class graph LAYER, not a boundary.** The
  Codebase-Memory paper (arXiv 2603.27277) and harness.io model a monorepo as
  layered edges: import graph, **test coupling** (a test file's imports of the
  code it exercises), and co-change. Their measured finding: **test coupling
  correlates more strongly with the import graph than co-change does** — i.e.
  test→source is a high-signal edge worth capturing explicitly. Nobody splits
  tests into a separate index; they tag test nodes and draw the edge.

Takeaway: the industry answer to both problems is the same — **one graph;
module membership and test-vs-source are node metadata + edges, derived from
the build manifests; "module" is a facet you filter/query, not a folder you
partition.**

## Decision (proposed) — three moves, smallest-blast-radius first

### Move 1 — a module is a NAME + a SET OF PATHS (config), not a single dir
Extend `.ctxoptimize/config.json` `modules[]` from `{path}` to accept
`{name, paths[]}` (globs allowed), so a module can gather scattered folders
into ONE store:
```json
{"modules": [
  {"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]},
  {"name": "Orders",  "paths": ["src/Orders",  "tests/Orders.Tests"]}
]}
```
The store key is the module NAME (no longer forced to mirror a dir); the gather
walks each path into that one store. Single-path `{path}` stays valid
(back-compat). This directly fixes "split across folders" and keeps
source+test in ONE store so call/import edges between them resolve (calls
resolve store-wide — separating them would sever test→source edges).
Store layout can no longer strictly mirror the tree → key by sanitized module
name; the navigator maps name → its paths.

### Move 2 — REASSIGNED to the agent skill (NOT in the binary)
The binary does NOT parse `.sln`/`settings.gradle`. Instead the **agent skill**
(the host intelligence) reads the solution/Gradle manifest during onboarding and
writes the `modules[]` path-sets into `.ctxoptimize/config.json` — same door as
any other config the skill maintains. This keeps the binary deterministic (no
manifest-derived module *guessing* in core; it only consumes explicit config)
and puts the fuzzy `.Tests`↔source pairing where an LLM belongs. The skill's
`references/onboarding.md` gains a ".NET solution / Gradle multi-project" recipe:
read the manifest, emit `{name, paths[]}` per project group, pair test projects.

### Move 3 — test projects become an EDGE layer, never a boundary
Tag test-project / test-file nodes (`is_test` metadata: `*.Tests.csproj`,
`*_test.go`, `test/**`, `__tests__`, `*.spec.ts`…) and emit a
**`tests` / `tested_by` edge** from a test symbol to the source symbol it
imports/calls (the research's "test coupling" layer — highest-signal). Rides
the existing `code` producer + `ProjectReference` (a `.Tests` project
referencing its source = the coupling, EXTRACTED). Answers "what tests cover
this?" and `affected <symbol>` now includes its tests. A new dashboard
kind/producer facet ("tests") filters them in the viewer.

## What we DON'T change
- Genuinely independent deployables (separate `go.mod`/`package.json` services)
  stay per-store, mirrored — that model is correct there; Move 1 is additive.
- No LLM, no DB, deterministic — all three moves are manifest parsing + AST
  tagging, same contract.

## Decisions (maintainer, 2026-07-14) ✅
- **Store keying:** name-keyed store when a module declares `paths[]`; the
  navigator maps name → its path set. (Chosen.)
- **Move 1 over CodeGraph-style one-graph facet:** Move 1 (multi-path store)
  is enough — fits our federation + pack model without a storage rewrite. (Chosen.)
- **`.sln`/Gradle parsing:** NOT in the binary — the agent skill does it and
  writes config (Move 2 reassigned). Test-pairing (`Foo.Tests`↔`Foo`) is the
  skill's job too. (Chosen.)

## Implemented (2026-07-14)
- **Move 1: DONE.** `scan.Module` gained `Paths[]` + `Multi()/KeySeg()/Dirs()/
  NSPrefix()`; `scan.Expand` resolves multi-path modules (name required, globs
  allowed, missing non-glob path fails loudly). `code.ExtractPaths(base, roots,
  exclude)` walks scattered dirs in ONE pass (base-relative IDs) so calls
  resolve across the split; `ExtractExcluding` delegates to it (byte-identical
  single-path). `gatherInto(base, dirs, …)` + `gatherMerged`/`prefixBatch` bake
  repo-root-relative IDs; multi-path stores are name-keyed and namespace with
  "" at read time. Federation, module-scope add/query/card, sync-prefix, and
  the navigator all branch on `KeySeg`/`NSPrefix`. Tests:
  `TestMultiPathModuleGathersScatteredFoldersIntoOneStore` (one store, repo-root
  sources, test→source call resolves) and `TestMultiPathModuleScopeFromSubdir`.
- **Move 2: DONE (in the skill, not the binary).** `references/onboarding.md`
  gained the ".NET/Gradle/Nx scattered-module" recipe + a SKILL.md routing row;
  the host agent reads the manifest and writes `{name, paths[]}`.
- **Move 3: NOT YET** — test-coupling `tested_by` edges remain a follow-on.

## Success checks (when built)
- A .NET repo with `src/Billing` + `tests/Billing.Tests` gathers as ONE
  `Billing` store; `card <SourceType>` lists its tests via `tested_by`;
  `affected <SourceType>` includes the test project.
- `scan` on a repo with a `.sln` proposes modules matching the solution's
  project set, spanning `src/` and `tests/`.
- Existing single-path / per-service monorepos behave exactly as today.

## Sources
falkordb.com/blog/code-graph · harness.io "your repo is a knowledge graph" ·
github.com/colbymchenry/codegraph · arXiv 2603.27277 (Codebase-Memory) ·
davidfowl .NET project structure gist · Microsoft `dotnet sln` docs · JetBrains
Rider solution docs.
