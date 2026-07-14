# Design notes — manifest lane (implemented 2026-07-14)

## The pack selector language is TINY, on purpose

A manifest pack rule is:

```json
{"file": "<basename glob>", "format": "json|xml|yaml",
 "path": "<selector>", "emit": "dependency|task", "namespace": "<ns>"}
```

The whole selector contract:

- **json / yaml** — dot path (`libraries.*`, `tools.build.deps`). Each
  segment matches a mapping key exactly; `*` matches every key (and, for
  json, every array element). Yield rules:
  - trailing `*` over a MAPPING → one hit per entry: name = key,
    version_spec = value when it is a string
  - path landing on a string scalar → that string is the name
  - path landing on a list of strings → one name per item
  - anything else (objects, numbers, booleans) → skipped silently
- **xml** — element path (`project/target`), `*` matches any element name,
  optional trailing `/@attr` takes the attribute value instead of the
  element's character content. Names only — no version channel for xml.
- `file` globs match the **basename** (path.Match). No `**`, no directory
  patterns.
- `namespace` defaults to the pack name; dep ids are `dep:<ns>/<name>`,
  tasks are `<file>::task:<name>` labeled `<ns>:<name>`.

**The boundary:** no predicates, no sibling lookups ("the version lives in
the entry's `ver` child"), no cross-file joins, no conditionals, no regex
capture. When a manifest needs any of that, the answer is an **adapter
script** through the validated `add --json` door — the universal escape
hatch — not a bigger selector language. A selector language that grows
predicates becomes a query engine; we already have one of those (the graph).

Pack validation is loud at add time (grammar/route-pack precedent). A
malformed USER file that a valid rule happens to match is skipped silently —
the pack was the claim; the user's data file is not ours to fail an add over.

## Gradle is line-shape matching, NOT a Gradle model

`build.gradle` / `build.gradle.kts` are programs. Recognized: single-line
`implementation|api|testImplementation|runtimeOnly|compileOnly` with one
quoted `'group:artifact:version'` literal (single/double quotes, parens or
not — covers the Kotlin DSL). Everything else is skipped silently:
interpolation (`"$ver"`, `"${…}"`), version catalogs (`libs.foo.bar`),
`project(':x')`, `platform(...)`, variables, dependency blocks built in
loops. Literal-or-silent, same contract as the route recognizers. Gradle
coordinates land in the `dep:maven/` namespace so the same library federates
across pom.xml and gradle modules.

## Other recorded limitations (v1)

- Helm-templated yaml (`{{ }}` anywhere in the file) is skipped whole —
  templated yaml lies to static parsers. No kustomize overlay evaluation.
- Lockfiles are skipped (data, not intent); no version resolution ever.
- k8s Secret resources: identity node only (kind/name/namespace) — `data:`
  is never read. Secret-smelling FILENAMES are refused entirely, both for
  core recognizers and pack rules.
- `.sln` parsing is line-based (the `Project(...) = ...` list); solution
  folders are filtered by the missing project-file extension.
- Federation of a shared dep id across module stores is store-level (same
  id in each store); the namespaced federated read path still prefixes ids
  per module — dedup at that layer is future work if it earns itself.
