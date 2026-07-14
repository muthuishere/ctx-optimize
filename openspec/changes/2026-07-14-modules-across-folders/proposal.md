# ADR — modules are manifest-defined path SETS, not folder subtrees; test projects are edges, not boundaries

Status: DRAFT v1 — research + design, maintainer discussing 2026-07-14. Nothing
implemented. This challenges a load-bearing assumption (module == directory
subtree) and needs a maintainer decision before build.

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

### Move 2 — `.sln` / `settings.gradle` derive modules automatically
Teach `scan` (or a small manifest reader) to parse a `.sln` (and later
`settings.gradle`, Nx `workspace`) into modules-as-path-sets: each solution =
one module whose paths are the `.csproj` dirs it lists; solution folders can
group further. So `scan` on a .NET repo proposes REAL modules matching the
solution, not folder guesses. `.csproj` alone (no `.sln`) → each project a
path; source+test paired by the `.Tests` naming convention when no manifest
says otherwise (heuristic, INFERRED, overridable).

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

## Open questions (maintainer ⚖️)
- Store keying when a module spans folders: name-keyed store vs. keep mirrored
  and add a "members" index. (Leaning name-keyed.)
- Do we also want the CodeGraph-style "one graph, module = queryable facet"
  as an alternative to per-module stores for the .NET case, or is Move 1
  (multi-path store) enough? (Leaning Move 1 — it fits our federation + pack
  model without a storage rewrite.)
- Test-pairing heuristic when no `.sln`: `Foo.Tests`↔`Foo` by name — good
  enough, or require the manifest? (Leaning heuristic + override.)

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
