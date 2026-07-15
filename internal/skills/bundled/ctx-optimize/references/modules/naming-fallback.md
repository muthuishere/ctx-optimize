# Go, Rust, and no-manifest repos — convention & workspace fallback

When there's no reactor-style manifest to parse, derive modules from the
language's workspace file or from folder/naming conventions.

## Go — `go.work` or one module per `go.mod`

- `go.work` lists member modules:
  ```
  go 1.22
  use (
      ./services/billing
      ./services/orders
      ./shared
  )
  ```
  Each `use` path is a module directory → single-path module (Go keeps
  `_test.go` files beside the source, so no split):
  ```json
  {"name": "acme", "modules": [
    {"name": "billing", "path": "services/billing"},
    {"name": "orders",  "path": "services/orders"},
    {"name": "shared",  "path": "shared"}
  ]}
  ```
- No `go.work`? Find every `go.mod` in the tree; each is an independent module,
  keyed by the last path segment of its `module` line.

## Rust — `Cargo.toml` `[workspace]`

```toml
[workspace]
members = ["crates/*", "apps/cli"]
```
Expand the `members` globs; each crate dir (has its own `Cargo.toml`) is a
single-path module. Tests live in `tests/` INSIDE the crate — no split.

## Anything else — naming convention

No workspace manifest at all (loose folders, a language ctx-optimize covers via
its own detection). Group by name:

1. `scan --json` to see what the binary would produce (one module per detected
   build root / language cluster).
2. Where a source dir has a matching test dir in a SEPARATE location, pair them
   into a multi-path module. Recognized test-dir conventions:
   - `src/Foo` ↔ `tests/Foo`, `test/Foo`, `Foo.Tests`
   - `foo/` ↔ `foo_test/`, `foo-tests/`, `__tests__/foo`
   - `pkg/foo` ↔ `test/foo`, `spec/foo`
3. Otherwise accept `scan`'s single-path modules as-is.

```json
{"name": "acme", "modules": [
  {"name": "Foo", "paths": ["src/Foo", "tests/Foo"]},
  {"name": "Bar", "path":  "src/Bar"}
]}
```

## When in doubt

A single-module repo needs NO `modules[]` at all — plain `init && add .` is
right. Only introduce `modules[]` when the tree genuinely holds several build
units OR a module's source and tests are split across top-level folders. Always
show the user your inferred grouping before writing.
