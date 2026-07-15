# Maven — parse a reactor `pom.xml` into modules

A Maven multi-module build lists its children in the root `pom.xml`
`<modules>` block. Each `<module>` is a RELATIVE directory holding its own
`pom.xml`.

## 1. Read the reactor

```xml
<!-- root pom.xml -->
<project>
  <artifactId>acme-parent</artifactId>
  <packaging>pom</packaging>
  <modules>
    <module>billing</module>
    <module>orders</module>
    <module>gateway</module>
  </modules>
</project>
```

Each `<module>X</module>` → directory `X/` (relative to the pom's dir).
Modules can nest: a child module may itself be `packaging=pom` with its own
`<modules>` — recurse.

## 2. Standard layout → single-path

Maven keeps tests inside the module (`billing/src/main/java` +
`billing/src/test/java`), so each reactor module is normally single-path — the
whole module directory:

```json
{"name": "acme", "modules": [
  {"name": "billing", "path": "billing"},
  {"name": "orders",  "path": "orders"},
  {"name": "gateway", "path": "gateway"}
]}
```

Use the module's `<artifactId>` (from its own pom) as the `name` if it's more
meaningful than the folder name.

## 3. Multi-path only for split test modules

Some teams factor integration tests into a separate reactor module
(`billing-it/`) that depends on `billing`. Fold it in:

```json
{"name": "billing", "paths": ["billing", "billing-it"]}
```
Detect via the test module's `<dependency>` on the source module's
`artifactId`, or the `-it`/`-tests`/`-test` suffix.

## 4. Confirm and gather

Show the reactor → module mapping, let the user adjust, write
`.ctxoptimize/config.json`, then `ctx-optimize add .`.
