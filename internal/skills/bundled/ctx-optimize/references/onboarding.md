# Onboarding a repo — set it up (scan → confirm → init → add)

This is the SETUP side. Querying a store that already exists is the routing
table; building one for the first time is here. `.ctxoptimize/` is the ONLY
thing written into the user's repo — commit it so the whole team inherits it.

## FIRST: is this repo already set up? (pull, don't rebuild)

Before ANY init/scan/add, check whether the repo already carries a committed
`.ctxoptimize/config.json` with a `remote`. If it does, a teammate already
BUILT and PUBLISHED the store — your job is to FETCH it, not re-derive it:

```
ctx-optimize remote pull      # fills the local store from the team's prebuilt graph
ctx-optimize status --json    # confirm nodes > 0
```

`ctx-optimize init` now detects this itself: on a clone with a remote-configured
config and an empty local store, it prints the `remote pull` line and does
NOTHING else — do not "fix" that by running `add`. A clone should PULL, never
init+add (that would rebuild the graph from source and diverge from the team's).
Only fall through to the build steps below when there is NO committed config
(genuinely new repo) — or when the user explicitly wants a local re-index
(`ctx-optimize init --force` then `add .`).

## Single project (one build root, one language stack)

```
ctx-optimize init && ctx-optimize add .
```

`init` scaffolds `.ctxoptimize/config.json` (name + adapters dir + example
pack samples) and writes the AGENTS.md/CLAUDE.md pointer block. `add .`
gathers code + markdown + manifests + routes + git co-change in seconds.
Confirm with `ctx-optimize status --json` (nodes > 0) and one `query`.

## Monorepo — you MUST scan and confirm before you build

Never init a monorepo blind: one giant graph is wrong, and you don't know the
module list. Drive this exact loop:

1. **Scan (read-only):** `ctx-optimize scan --json` (`--depth N` if the tree
   is deep; default 5). It finds every project by build-file markers —
   nothing is written.
2. **Show the user the FULL found list** — every module, not a sample — and
   ask them to confirm. If something's missing, re-scan deeper or add globs;
   if something's noise, they'll drop it from the config after. Do NOT skip
   this and do NOT silently build a single graph.
3. **On their okay:** `ctx-optimize init --scan --yes` — writes every found
   module into `config.json` `modules[]` (the user owns the list afterward)
   and scaffolds the root.
4. **Gather:** `ctx-optimize add .` at the root fans out one worker per
   module in parallel (`--jobs N` to tune), building one store per module
   plus the root navigator (`modules.json` + `navigator.md`).

## Scattered modules — source and tests in SEPARATE folders (.NET, Gradle, Nx)

`scan` finds a module per build-file, mirroring folders. That is wrong when the
BUILD SYSTEM, not the filesystem, defines a module — the classic case being
.NET (`src/Billing/Billing.csproj` + `tests/Billing.Tests/Billing.Tests.csproj`
are ONE logical module in two top-level folders) and Gradle/Nx multi-projects
whose `projectDir`s are scattered. Splitting them into two stores severs the
test→source call edges.

**This is YOUR job, not the binary's** — the binary stays deterministic and only
consumes explicit config; you are the intelligence that reads the manifest and
figures out the grouping. Scan the repo at whatever depth it takes, read the
`.sln` / `settings.gradle` / `nx.json` / `project.json` (or infer from the
`Foo` ↔ `Foo.Tests` naming when there's no manifest), decide which scattered
folders belong to one module, and write a **multi-path module** into
`.ctxoptimize/config.json` — a NAME plus a SET of paths, gathered into ONE
name-keyed store (IDs go repo-root-relative; code extracts in a single pass so
test→source calls resolve):

```json
{"name": "acme", "modules": [
  {"name": "Billing", "paths": ["src/Billing", "tests/Billing.Tests"]},
  {"name": "Orders",  "paths": ["src/Orders",  "tests/Orders.Tests"]}
]}
```

Recipe: (1) locate the solution/settings manifest; (2) list its projects with
their dirs; (3) group each source project with its test project(s) — by the
manifest's `ProjectReference`/`include`, else by the `*.Tests`/`*_test`/`*.spec`
name convention; (4) emit one `{name, paths[]}` per group; (5) `add .`. Paths
may glob (`"tests/*.Tests"`). Single-path `{"path": "..."}` still works and is
right for genuinely independent deployables (a service per `go.mod`). Show the
user the grouping you inferred before writing — you own the guess, they own the
final list.

## After onboarding

- Verify: `status --json`, then a `query` — cite a real hit back to the user.
- Customize if the graph misses their routes / deps / k8s / language →
  `./references/customize.md` (check `routes/manifests/languages list` first).
- Non-code sources (DB, docs, queues) → `./references/adapters.md`.
- Share the store with the team → `./references/push-pull.md`.
- Querying the built monorepo (scope-follows-cwd, navigator, merge) →
  `./references/multi-module.md`. Onboarding builds it; that guide drives it.
- Interactive setup instead of the CLI? `ctx-optimize serve` → Repos tab
  onboards, re-gathers, and removes repos from the dashboard
  (`./references/dashboard.md`).
