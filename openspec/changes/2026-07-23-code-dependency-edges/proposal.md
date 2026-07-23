# ADR — code → dependency linking: bridge `module://` to `dep:`, normalize declaration scope

Status: IMPLEMENTED 2026-07-23 — owner-approved after the spikes. Moves 1–3
shipped: `internal/extract/deplink` (linker + go-self skip), `scope_class`
on declares edges + `scopes` aggregate on dep nodes
(`internal/extract/manifests/scopeclass.go`), wired into `gatherInto`.
`task ci` + `task golden` green; verified end-to-end on this repo (22
resolves_to edges, `dep:npm/react` scopes=runtime, `dep:npm/typescript`
scopes=dev, `affected dep:npm/react` reaches importing files).
Open question 5 (workspace-internal imports) deferred as agreed.
Trigger: issue #5 (elan-chels, 2026-07-23) — external-usage analysis for a
"solution architecture drift" consumer over `export --format json`.

## Context — what the issue asks, and what already exists

The issue reports two gaps on 0.3.7:

1. "No code → dependency import edges" — imports edges never target
   `dep:` nodes, so the graph can't answer "which files use package X".
2. "Dependency nodes carry no scope" — `dep:` node metadata is only
   `{ecosystem, producer}`, so `@types/*`/`typescript` can't be told apart
   from runtime integrations without a hardcoded ignore-list.

Verified against the source, the real state is narrower on both counts:

- **The code lane already emits external import edges.** Every import
  statement becomes `file --imports--> module://<specifier>`
  (internal/extract/code/code.go L647-L660, target text via `importTarget`
  L738-L757: last named child, unquoted — `"fmt"`, `'react'`, `<stdio.h>`,
  `java.util.List`). The reporter's "139 imports edges, all internal" is
  those `module://` nodes — they exist, but **nothing connects
  `module://react` to `dep:npm/react`**. Two id namespaces, one from each
  producer, no bridge. That is the actual gap.
- **Scope already ships — on the `declares` EDGE, since the manifest
  lane's first commit** (fd86063, in v0.3.0; the reporter's 0.3.7 has it).
  `collector.declares` (internal/extract/manifests/manifests.go L86-L98)
  stamps `version_spec` + `scope` into edge metadata, and `schema.Edge.
  Metadata` serializes in `export --format json` (app.go L1796 marshals raw
  schema structs). npm emits `dependencies|devDependencies|
  peerDependencies` (npm.go L33-L49), go.mod `require|indirect`, maven
  `compile|test|…|parent|plugin`. The reporter looked at NODE metadata and
  never found it — a **discoverability/normalization** gap, not a missing
  feature: the vocabulary is raw per-ecosystem section names, and it lives
  one edge away from where a consumer looks.

Placement of scope on the edge is correct and stays: the same dep can be
`devDependencies` in one manifest and `dependencies` in another — scope is
a property of the *declaration*, not the package.

## Decision (proposed) — three moves, smallest blast radius first

### Move 1 — a `deplink` linker: `module://X --resolves_to--> dep:ns/name`

A small cross-lane linker that runs inside `gatherInto` AFTER the code and
manifests producers of the same gather, sees both in-memory batches, and
emits bridge edges under **its own producer name `deplink`** with its own
Replace lifecycle (house rule: cross-producer facts get their own producer;
neither source lane is touched, and link churn prunes independently).

Edges: `module://<specifier> --resolves_to--> dep:<ns>/<name>`,
confidence **INFERRED** + `synthesized_by: deplink` (matched by
computation, per the provenance discipline in the manifest-lane ADR), edge
metadata `ecosystem: <ns>`.

Resolution rules — exact where the specifier IS the package name,
conservative everywhere else (the calls discipline: ambiguous is dropped,
not guessed):

| ecosystem | rule |
|---|---|
| npm | strip subpath: `react-dom/client` → `react-dom`, `@scope/pkg/sub` → `@scope/pkg`; skip relative (`./`, `../`) and `node:*` builtins; exact match against `dep:npm/<name>` |
| go | longest-prefix match of the import path against `dep:go/<modpath>` (`github.com/x/y/pkg/z` → `dep:go/github.com/x/y`); skip stdlib (no dot in first path segment) AND the repo's own `module` path from go.mod (spike finding 1 — self-imports are internal edges already) |
| maven/gradle (java imports) | package prefix vs `groupId` of `dep:maven/<g>:<a>`; link ONLY when exactly one dep matches — else drop |
| pypi | import name vs PEP 503-normalized dist name, exact match only (accepts partial coverage — `import yaml` / PyYAML stays unlinked rather than guessed) |
| nuget (c# usings) | namespace prefix vs package id, unambiguous-only, same as maven |

Consumer answers after this move: "which files use package X" =
`file --imports--> module://* --resolves_to--> dep:X` (2 hops);
`affected dep:npm/react` reaches importing files at its default depth 2 —
the issue's "blast radius of swapping a package" works with zero verb
changes.

### Move 2 — normalize scope: `scope_class` beside raw `scope`

Keep the raw section name; add a normalized class on the same `declares`
edge: `scope_class: runtime | dev | peer | optional | test | build |
indirect`. Mapping table per recognizer (npm dependencies→runtime,
devDependencies→dev, peerDependencies→peer; go require→runtime,
indirect→indirect; maven compile/runtime→runtime, test→test,
provided→build, plugin/parent→build; gradle configuration names likewise;
nuget PackageReference→runtime, PrivateAssets=all→build). One vocabulary a
consumer can filter on without knowing five ecosystems' section names.

### Move 3 — mirror an aggregate onto the dep node (convenience)

`dep:` node metadata gains `scopes`: the sorted, comma-joined union of
`scope_class` across that batch's `declares` edges (`"dev"`,
`"dev,runtime"`). Computable inside the manifests collector at batch close —
no cross-producer read. The EDGE stays the authority; the node field is a
one-look convenience for exactly the issue's consumer (filter dep nodes by
`scopes` without walking edges).

## What this deliberately does NOT do

- No direct `file --imports--> dep:` edges (the issue's "reuse imports"
  option): it would duplicate every per-file edge the `module://` pivot
  already aggregates, and mix computed facts into the code lane's
  EXTRACTED namespace. The 2-hop path is the graph-shaped answer. (Open
  question 2 if consumers push back.)
- No lockfile reading, no node_modules scanning — manifests stay intent,
  per the manifest-lane ADR.
- No version resolution — `version_spec` on the declares edge remains the
  only version fact.

Implementation note (spike finding 2): resolution picks the ecosystem from
the importing file's language — never try-every-ecosystem per specifier.

## Performance — MEASURED, see spikes.md

Two spikes (2026-07-23, `spikes.md` beside this file) ran the exact rules
above against real stores:

- **This repo** (go+npm, 2,956 nodes): 22 links, **0.286 ms**, +0.39% edges;
  all 32 unresolved were self-module imports → the go self-skip rule.
- **mastra-main** (242-module TS monorepo store, 160k nodes / 220k edges):
  3,632 links = 84% of external candidates, **1.4 ms TOTAL** (~6 µs/module),
  +1.65% edges; the 704 unresolved are workspace-internal imports (open
  question 5).

Why it stays that cheap by construction:

- **Gather**: the linker is in-memory map matching over the two batches
  `gatherInto` already holds — no walk, no parse, no store read. npm = one
  hash lookup per distinct `module://` node; go = prefix scan over the few
  `go.mod` entries; java/py/nuget = same maps, unambiguous-only.
  Microseconds against a code lane that costs ~0.5s per 4k files, and it
  parallelizes per-module like every other lane.
- **Store**: zero new nodes (`module://` and `dep:` both already exist);
  new edges are bounded by distinct resolvable external packages, not by
  files — the issue reporter's store would gain ≤14 edges on 2956 nodes.
  Moves 2–3 are metadata strings on existing edges/nodes.
- **Query**: `scoreNode` iterates nodes; node count unchanged. Neighbor
  inlining sees at most one extra `resolves_to` edge on module nodes.
- **`affected` on a hub dep** now fans out to importing files — that is
  the requested feature, and section caps are reported via `Truncated`,
  never silent.
- **Enforcement, not assertion**: the golden net pins perf ceilings and
  speed may only move up — a gather/query regression fails `task golden`
  before merge.

## Open questions for the owner

1. Confidence of exact-algorithm links (npm/go): INFERRED + synthesized_by
   per the letter of the provenance rule, or EXTRACTED since the mapping is
   deterministic and lossless? Draft says INFERRED — the fact is still
   synthesized across two files.
2. Is the 2-hop answer acceptable for the issue's consumer, or do we also
   want a `--expand deplink` style flattening in `export`? (Defer until
   asked twice.)
3. `resolves_to` vs reusing `depends_on` for the bridge relation. Draft
   picks a new relation — `depends_on` already means project→project and
   workspace membership; overloading it muddies `affected` filters.
4. Reply on #5 pointing at the existing `declares` edge scope metadata now
   (it unblocks half their analysis on their current version), before any
   of this ships?
5. Workspace-internal imports (spike 2: 704 of 4,336 candidates in mastra —
   `@mastra/*` imported without being declared in the importing module's
   manifest): resolve them against the workspace-member set (the
   `depends_on` workspace edges already exist), leave unlinked (draft), or
   surface later as a lint-grade "undeclared dependency" fact? Note this IS
   the monorepo version of the reporter's drift signal.

## Success check

- Golden: a fixture repo (js + go modules) where `query`/`affected` answer
  "which files use react" and "blast radius of dropping lodash" with cited
  edges; scoreboard floors may only move up.
- The reporter's own probe passes: `export --format json` shows imports →
  module → dep connectivity and filterable scope without an ignore-list.
