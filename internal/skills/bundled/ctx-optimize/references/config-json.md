# Creating `.ctxoptimize/config.json` — the committed contract

The config file is a **separate step** from gathering. `init` scaffolds a minimal
one; you (the agent) enrich it — especially the `modules[]` list for a
multi-project repo, which only YOU can derive by reading the build system.
`add` then consumes it deterministically. Commit the file: the whole team's
agents inherit the same layout.

## Where it lives

`<repo-root>/.ctxoptimize/config.json`. The CLI walks up from cwd to find it, so
a module subfolder inherits the root config. `init` creates the `.ctxoptimize/`
dir with `config.json` + `adapters/` (+ an inert `example.js.sample`).

## Full schema (every field optional except by feature)

```json
{
  "name": "acme",
  "remote": {"push": "node .ctxoptimize/push.js", "pull": "node .ctxoptimize/pull.js"},
  "modules": [
    {"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]},
    {"name": "gateway", "path": "services/gateway"}
  ],
  "instructions": "ALL",
  "skills": "ALL",
  "hooks": "ALL"
}
```

| Field | Meaning |
|---|---|
| `name` | Store key under `~/ctxoptimize/<name>/`. Defaults to the repo dir basename. Set it when the folder name is generic (`app`, `backend`). |
| `remote` | The push/pull transport COMMANDS (`{"push": "<shell line>", "pull": "<shell line>"}`) — the binary ships no transport; your committed script is the remote (see `push-pull.md`). Legacy v0.3 URL forms load inert. Secrets stay env-var NAMES; the shell expands them at run time. |
| `modules[]` | The multi-project layout. Each entry is EITHER single-path `{"name","path"}` (one deployable/build root) OR **multi-path** `{"name","paths":[...]}` (scattered folders — src + tests — that are ONE logical module). Absent → the repo is one single-module store. |
| `instructions` | Which agent files `init` writes the pointer block into: `CLAUDE` \| `AGENTS` \| `ALL` \| `NONE`. |
| `skills` / `hooks` | Which dirs/platforms `install` targets: `CLAUDE` \| `AGENTS` \| `ALL` (`hooks` also `NONE`). |

## The two module shapes — pick per module

- **Single-path** `{"name":"gateway","path":"services/gateway"}` — a genuinely
  independent deployable (a service per `go.mod`, one app per folder). This is
  what plain `scan`/`init --scan` writes: one module per build file.
- **Multi-path** `{"name":"Billing","paths":["src/Billing","tests/Billing.Tests"]}`
  — when the BUILD SYSTEM (not the filesystem) defines the module and its files
  are scattered (.NET `src/`+`tests/`, Gradle/Nx projects with split dirs). All
  paths gather into ONE name-keyed store in a single pass, so **test→source
  calls resolve across the split**. Paths may glob: `"tests/*.Tests"`.

> Splitting a module's source and tests into two stores severs the test→source
> edges. When in doubt, group them.

## How to fill `modules[]` for a multi-project repo

1. **Detect the build system** and open `./modules/index.md` — it routes you to
   the exact parser asset (`.sln`, Gradle, Maven, JS workspaces, or the
   naming-convention fallback).
2. Parse the manifest → list every project with its directory.
3. Group source projects with their test project(s) (by the manifest's
   reference/`include`, else the `*.Tests`/`*_test`/`*.spec` naming convention).
4. Emit one `{name, paths[]}` per group.
5. **Show the user the grouping you inferred and let them correct it** — you own
   the guess, they own the final list.
6. Write the config, then `ctx-optimize add .` (multi-module fans out one worker
   per module + builds the root navigator).

## Editing an existing config

It's a plain committed JSON file — edit it directly (add a module, fix a path,
add a remote), then `add .`. `init --scan` regenerates `modules[]` from a fresh
scan (single-path per build file); your hand-authored multi-path groups are
yours to maintain after that first generation.
