# ADR — manifest lane: build tools + k8s topology as first-class graph, core + packs

Status: DRAFT v1 — owner-approved direction 2026-07-14 ("i need build tools
separately as well … similar like routes"); implementation NOT started.
Sequencing: blocked on W4 (frontend/custom/yaml routes) merging first — both
touch the config extraction surface.

## Context — today these files are indexed, not understood

The markdown producer's config lane (d8ac0cc) indexes manifests as a
document node + line-anchored TOP-LEVEL keys only:

- `package.json` → keys like "name", "dependencies" — but not one node per
  dependency, no versions, no script tasks.
- `pom.xml` → effectively nothing (XML doesn't match the `key: / key=` line
  scan).
- `*.csproj` / `*.sln` → **not matched at all** (not in manifestNames, not a
  configExt).
- `build.gradle` → raw lines at best.
- K8s manifests → recognized as yaml config docs; no resource nodes, no
  topology edges. (W4 adds ingress ROUTE nodes — the route side only.)

Meanwhile dependency questions are among the highest-frequency agent
questions ("what version of X", "which modules use our internal lib", "what
breaks if I bump Y") and their answers are EDGES — cross-module ones. Grep
gives occurrences; only the graph gives the module→dep→module picture.

## Decision — a separate `manifests` producer, core + packs (the doctrine)

The extension doctrine (established 2026-07-14 for routes, generalizing
grammar packs): **core embedded for shapes that need code; drop-in packs for
everything declarative; CLI verbs to list/add/remove; GitHub-URL install.**

### Producer

New package `internal/extract/manifests`, producer name `manifests` — its
own batch, its own producer-scoped Replace lifecycle (dependency churn
prunes independently of code/docs). Wired into gatherInto beside
markdown/code/githistory; always-Replace (the emptied-module lesson, 65ee1d3).
The markdown config lane KEEPS its shallow document+key indexing (searchable
text is still useful); the manifests producer adds the SEMANTIC layer. Node
id namespaces must not collide (dep:/task:/k8s:// vs file paths — disjoint
by construction).

### Node/edge shape

| thing | node | notes |
|---|---|---|
| external dependency | id `dep:npm/express`, `dep:maven/org.apache.kafka:kafka-clients`, `dep:go/github.com/x/y`, `dep:nuget/Newtonsoft.Json` | kind "dependency", file_type "manifest"; version in metadata (`version_spec`), NOT in the id — the same lib at two versions is one node with two edges carrying their own version metadata |
| declared task/script | id `<file>::task:build`, label `npm:build` / gradle task name | kind "task"; the script body line-anchored |
| k8s resource | id `k8s://<ns>/<kind>/<name>` | kind lowercased ("deployment", "service", "configmap"…); namespace "default" when absent |
| edges | manifest file --declares--> dep (EXTRACTED, metadata version_spec, scope/configuration); module/project --depends_on--> project (csproj ProjectReference, maven modules, npm workspaces — EXTRACTED); service --selects--> deployment (label match — INFERRED, synthesized_by k8s-selector); ingress --routes_to--> service (EXTRACTED); deployment --mounts--> configmap/secret-resource (EXTRACTED); container --uses_image--> `image:<ref>` (EXTRACTED) | provenance discipline as everywhere: in-the-file = EXTRACTED; matched-by-computation = INFERRED + synthesized_by |

### Core five + k8s (embedded recognizers)

1. **package.json** — stdlib encoding/json: dependencies/devDependencies/
   peerDependencies (scope in edge metadata), scripts → task nodes,
   workspaces → depends_on edges to member module manifests.
2. **pom.xml** — stdlib encoding/xml: dependencies (groupId:artifactId,
   version incl. `${property}` left verbatim as spec text), modules →
   depends_on, parent edge, plugins as deps with scope "plugin".
3. ***.csproj / *.sln** — stdlib encoding/xml: PackageReference →
   dep:nuget/..., **ProjectReference → depends_on between projects** (the
   .NET module graph for free); .sln parsed line-based for project list.
   ADD .csproj to the walk's matched files (today it's invisible).
4. **go.mod** — line-based (require blocks): dep:go/... with version.
5. **build.gradle / build.gradle.kts** — the honest hard case: Groovy has no
   grammar in our set. Line-shape matching for the common forms
   (`implementation|api|testImplementation|compileOnly '<g>:<a>:<v>'`, same
   with double quotes and parens; kotlin-DSL equivalents). Anything dynamic
   is skipped silently — literal-or-silent, same as routes. NOT a full
   Gradle model; say so in docs.
6. **K8s manifests** — the yaml indent-walker (extended from W4's lane C,
   shared helper): any yaml with `kind:` + `apiVersion:` + `metadata.name`
   → resource node; the edge set from the table above. Multi-doc files
   (`---`) supported. Secret-kind resources: node only, data NEVER read
   (existing refusal discipline). Helm templates (`{{ }}` present) are
   skipped whole in v1 — templated yaml lies to static parsers; recorded
   limitation.

Lockfiles (package-lock.json, go.sum, *.lock) are SKIPPED in v1 — they are
data, not intent, and most exceed maxConfigBytes anyway.

### Manifest packs (declarative — this lane genuinely suits tables)

Unlike route recognizers (W1: real frameworks need visitor state), custom
manifests are usually "a path in a structured file" — declarative-friendly.
Pack file `<name>.json` in `.ctxoptimize/manifests/` (repo, committable) or
`<store-root>/manifests/` (machine); repo wins on name collision; malformed
packs fail loudly:

```json
{
  "name": "internal-deps",
  "rules": [
    {"file": "*.deps.json", "format": "json", "path": "libraries.*",
     "emit": "dependency", "namespace": "internal"},
    {"file": "*.build.xml", "format": "xml", "path": "target/@name",
     "emit": "task"}
  ]
}
```

`format`: json | xml | yaml (the three parsers we already carry). `path`: a
deliberately TINY selector language — dot path with `*` wildcard for json/
yaml, element path with `/@attr` for xml. If the tiny language can't express
it, the answer is an adapter script (the universal door), not a bigger
language — record this boundary in design.md.

### CLI verbs (mirror `languages` / `routes`)

`manifests list` (core + packs with source locations) · `manifests add
<name|github-url>` (scaffold with `_review` marker, or fetch+validate+install
from a repo URL / raw URL — reuse the routes/grammar URL handling; --global
for machine level) · `manifests remove <name>`.

## Success checks

- On a fixture monorepo (npm workspace + maven multi-module + a csproj
  pair + k8s manifests): `query "kafka-clients"` → the dep node with both
  declaring modules as neighbors; `affected dep:npm/express` → every module
  that declares it; `card k8s://default/service/api` shows selects/routes_to
  neighbors; csproj ProjectReference produces depends_on.
- False-positive guards: this repo's own Taskfile.yml/.goreleaser.yaml gain
  NO dep/k8s nodes; a random yaml with `kind:` but no apiVersion stays a
  plain config doc.
- Idempotent re-add; removing a dependency from package.json prunes its
  declares edge (producer-scoped Replace proof).
- Gather time on beam within +10% (manifests are few and small; the walk
  already visits them).

## Non-goals (v1)

- No version RESOLUTION (no ranges evaluated, no lockfile truth) — spec text
  verbatim.
- No Helm template evaluation; no kustomize overlays.
- No transitive dependency expansion — declared edges only.
- No network fetches of registry metadata — ever (contract).
