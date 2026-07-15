# Gradle — parse `settings.gradle(.kts)` into modules

Gradle declares subprojects in `settings.gradle` (Groovy) or
`settings.gradle.kts` (Kotlin). A subproject's directory is conventionally its
path with `:` → `/`, but can be REMAPPED — you must honor the remap.

## 1. Read the include list

```groovy
// settings.gradle
rootProject.name = 'acme'
include 'billing', 'orders', 'shared'
include ':services:gateway'
```
```kotlin
// settings.gradle.kts
include("billing", "orders")
include(":services:gateway")
```

Collect every path passed to `include(...)`. A leading `:` is the root; each
`:` thereafter is a directory separator. So `:services:gateway` →
`services/gateway`, and `billing` → `billing`.

## 2. Honor directory remaps

Gradle lets a project override its dir:

```groovy
project(':gateway').projectDir = file('services/api-gateway')
```
```kotlin
project(":gateway").projectDir = file("services/api-gateway")
```

Scan for `project(':X').projectDir = file('...')` and use that dir for `X`
instead of the default. Without a remap, the default (`:` → `/`) holds.

## 3. Pair source ↔ tests

Gradle's standard layout keeps tests INSIDE the subproject
(`billing/src/main/...` + `billing/src/test/...`), so a subproject is usually a
single-path module — the whole `billing/` folder:

```json
{"name": "acme", "modules": [
  {"name": "billing", "path": "billing"},
  {"name": "orders",  "path": "orders"},
  {"name": "gateway", "path": "services/api-gateway"}
]}
```

Use **multi-path** only when a project's tests live in a SEPARATE top-level
subproject (some teams split `:billing` and `:billing-test`, or an
`integration-tests/` project references many others):

```json
{"name": "billing", "paths": ["billing", "billing-test"]}
```
Detect this by a test subproject that `implementation project(':billing')` in
its `build.gradle` dependencies, or the `-test`/`-tests`/`.test` name suffix.

## 4. Included builds (composite)

`includeBuild('../shared-lib')` pulls a whole separate build. Treat each
included build as its own module (or its own repo entirely) — parse its own
`settings.gradle` the same way if you want its subprojects.

## 5. Confirm and gather

Show the module→path list, let the user correct remaps you may have missed,
write `.ctxoptimize/config.json`, then `ctx-optimize add .`.
