# JS/TS monorepos — Nx, pnpm/yarn/npm workspaces

JavaScript monorepos declare their packages in one of a few manifests. Read the
one that's present; each names package directories (often via globs).

## Detect which manifest

| File | Where the packages are listed |
|---|---|
| `pnpm-workspace.yaml` | `packages:` YAML list of globs |
| root `package.json` `"workspaces"` | array of globs (npm / yarn / bun) |
| `nx.json` (+ `project.json` per project) | Nx: each `project.json` marks a project root |
| `lerna.json` | `"packages"` globs (older monorepos) |

## Parse

- **pnpm** — `pnpm-workspace.yaml`:
  ```yaml
  packages:
    - "packages/*"
    - "apps/*"
    - "!**/dist"
  ```
  Expand each glob against the tree; each matched dir with a `package.json` is a
  package. Honor `!` negations.
- **npm/yarn/bun** — root `package.json`:
  ```json
  {"workspaces": ["packages/*", "apps/*"]}
  ```
  Same glob expansion. (`workspaces` may also be `{"packages":[...]}`.)
- **Nx** — find every `project.json` (or `package.json` with an Nx target);
  its directory is the project root. `nx.json` gives defaults, not the list.
- Read each package's `package.json` `"name"` for a good module `name`.

## Module shape

JS packages keep tests inside the package (`src/` + `__tests__/` or `*.test.ts`
beside the source), so each package is normally **single-path** — the package
dir:

```json
{"name": "acme", "modules": [
  {"name": "@acme/billing", "path": "packages/billing"},
  {"name": "@acme/orders",  "path": "packages/orders"},
  {"name": "web",           "path": "apps/web"}
]}
```

Use **multi-path** when a package's tests/e2e live in a SEPARATE top-level dir
(a common Nx pattern: `apps/web` + `apps/web-e2e`):

```json
{"name": "web", "paths": ["apps/web", "apps/web-e2e"]}
```
Pair by the `-e2e`/`-test`/`-tests` suffix or a `project.json` that references
the app.

## Skip build output

`node_modules`, `dist`, `build`, `.next`, `coverage` are already skipped by the
gatherer — don't list them as paths.

## Confirm and gather

Show package→path mapping, let the user adjust globs, write
`.ctxoptimize/config.json`, then `ctx-optimize add .`.
