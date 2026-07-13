# Tasks — multi-module init

Gate: `task ci` green before every commit.

## 1. Scan engine + verbs
- [x] internal/scan: bounded exhaustive marker walk, Options (depth/markers/
      include/exclude), clipped detection, tests
- [x] project.Config: Modules / ModuleOf / Scan fields
- [x] `scan` verb (tree + proposed config.json, --json)
- [x] `init --scan` (confirm unless --yes, writes FULL list, root-only
      scaffold), adoption rule for plain init inside declared module

## 2. Mirrored stores + fan-out add + navigator
- [x] store.SanitizeKeyPath (per-segment, keeps /), mirrored module keys
- [x] extractors: ExtractExcluding(root, excludeDirs)
- [x] add fan-out: worker pool (--jobs, min(NumCPU,8)), buffered ordered
      output, root residual, fail-closed; NESTED declared modules exclude
      each other (beam maven-archetypes case) and nested store dirs are
      skipped by the parent's manifest walk
- [x] internal/navigator: modules.json (pre-expanded paths, hubs top-100,
      README summary) + navigator.md + wiki front page
- [x] determinism test: --jobs 1 vs 8 byte-equal store trees + output

## 3. Resolution + federation
- [x] resolveScope walk-up (module_of > modules[] match > single);
      no-config fallback kept as today's basename behavior (error-on-missing
      deferred — would break `add` bootstrap ergonomics)
- [x] query: module scope, zero-hit escalation to root federation
      (K=3 concat via navigator Rank, --modules all|list, --root, widen-to-
      all on zero hits), scope labels; single scope byte-identical
- [x] card: module-miss escalates via federated retry, labeled
      "[not in X — found in Y]"; path/explain/affected get root-scope
      federated graphs via loadGraph
- [ ] affected/path boundary NOTES (cross-module truncation warnings)
- [x] remote push/pull scope prefixes: root = whole store tree (root store
      + every module store + navigator, stores.json index written last),
      module = only its prefix (nested stores included); single-module
      path byte-identical; prefix pull against an index-free single-store
      remote fails loudly

## 4. Skill + docs
- [x] SKILL.md: multi-module flow (scan→show FULL list→okay→init --scan
      --yes→add), scope-follows-cwd querying, merge stays opt-in
- [x] usage() documents scan / init --scan / add --jobs / query --modules
- [ ] README + agent-pointer block mention multi-module

## 5. Integration matrix
- [x] beam (310 modules found at depth 8, incl. nested maven modules):
      init --scan; fan-out add 14.5s / 885% CPU / 0 failures; module-scope
      query at sdks/java/transform-service labeled; root federation picked
      kafka modules via navigator; card escalation works
- [x] the-factory (no workspace manifests): 230 modules, bare-marker
      discovery (scan-only — real workspace, not mutated)
- [x] nexus-workspace: 37 modules (scan-only)
- [x] ctx-optimize itself: control — scan surfaces the 6 npm wrapper dirs
      (prune-at-confirm case); plain init/add/query unchanged, full suite
      green
