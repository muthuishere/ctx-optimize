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

## Multi-project repos — the build system defines the module (not folders)

`scan` finds one module per build file, mirroring folders. That is wrong when
the BUILD SYSTEM groups scattered folders into one logical module — the classic
case being .NET (`src/Billing/Billing.csproj` + `tests/Billing.Tests/…` are ONE
module in two top-level folders), and any Gradle/Maven/Nx project whose test dir
is separate. Splitting them into two stores severs the test→source call edges.

**This is YOUR job, not the binary's** — the binary stays deterministic and only
consumes explicit config; you read the manifest and derive the grouping, then
write `modules[]` into `.ctxoptimize/config.json`. Each build system has its own
parser asset — do NOT improvise:

- **How the config file works (schema, the two module shapes)** →
  `./config-json.md`
- **Detect the build system → parse it → group src+tests** →
  `./modules/index.md`, which routes to the exact parser:
  - `.sln` / `.csproj` → `./modules/dotnet-sln.md`
  - `settings.gradle(.kts)` → `./modules/gradle.md`
  - reactor `pom.xml` → `./modules/maven.md`
  - Nx / pnpm / yarn / npm workspaces → `./modules/js-workspaces.md`
  - `go.work` / Cargo workspace / no manifest → `./modules/naming-fallback.md`

The shape you emit per group: **multi-path** `{"name","paths":[src,tests]}` when
source and tests are split (they gather into one store, calls resolve), else
single-path `{"name","path"}` for a self-contained deployable. Always show the
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
