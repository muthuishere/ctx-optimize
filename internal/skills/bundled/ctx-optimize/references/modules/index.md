# Multi-project layout — detect the build system, then parse it

A repo is "multi-project" when one tree holds several build units. Plain
`scan`/`init --scan` writes one single-path module per build file — correct for
independent deployables, WRONG when the build system groups scattered folders
(source in `src/`, tests in `tests/`) into one logical module. Deriving those
groups is YOUR job (the binary stays deterministic and only consumes explicit
config). Each build system has its own parser asset below.

## Step 1 — detect (look at the repo root, in this order)

| You see… | Build system | Parser asset |
|---|---|---|
| `*.sln` (or many `*.csproj`/`*.fsproj`/`*.vbproj`) | .NET | `./dotnet-sln.md` |
| `settings.gradle` / `settings.gradle.kts` | Gradle | `./gradle.md` |
| root `pom.xml` with `<modules>` | Maven | `./maven.md` |
| `nx.json`, `pnpm-workspace.yaml`, or `package.json` `"workspaces"` | JS/TS workspace | `./js-workspaces.md` |
| `go.work`, `Cargo.toml` `[workspace]`, or NO manifest at all | Go / Rust / convention | `./naming-fallback.md` |

Several may be present (a polyglot monorepo) — run each relevant parser and
concatenate the `modules[]` they produce.

## Step 2 — parse

Follow the matched asset. Each one tells you how to list projects and their
directories, and how to pair a source project with its test project(s).

## Step 3 — group and decide the module shape

For each logical unit:
- Source + its test folder(s) in SEPARATE top-level dirs → **multi-path**
  `{"name","paths":[src, tests]}` (one store; test→source calls resolve).
- A self-contained deployable in one folder → **single-path** `{"name","path"}`.

Pairing rule when the manifest doesn't state it explicitly: match `Foo` with
`Foo.Tests` / `Foo.Test` / `foo_test` / `foo.spec` / `test-foo` by name.

## Step 4 — confirm, write, gather

1. Print the inferred grouping (module name → paths) to the user; let them edit.
2. Write `modules[]` into `.ctxoptimize/config.json` (schema: `../config-json.md`).
3. `ctx-optimize add .` — fans out one worker per module + builds the navigator.
4. Verify: `ctx-optimize status --json`, then a cross-module `query` (e.g. a
   test calling a source symbol) to confirm the split resolved.

Common trap: don't create a module per `.csproj`/subproject blindly — that
re-severs src/tests. Group first, then emit.
