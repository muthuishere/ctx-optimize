# Monorepos — one graph per module, plus a navigator

## Why per-module stores (and not one big graph)

- **Refresh cost tracks the change**: edit one service, only its store
  re-gathers (~1–2s) — not the whole monorepo.
- **Scope follows your cwd**: asking inside `services/api` answers from
  api's store, not ranked against every other module's noise. Zero hits
  escalate repo-wide automatically.
- **Safety checks stay trustworthy**: the >50% shrink guard only means
  something where scope is stable — which is exactly per-module.
- **The cross-module view isn't lost**: the root **navigator** federates
  queries across modules; `merge` materializes one combined store when you
  really want a single artifact.

## Getting started

```sh
cd my-monorepo
ctx-optimize up          # detects a monorepo, scans, writes config, gathers
```

That's the whole onboarding. For control over the module list:

```sh
ctx-optimize scan                 # read-only preview: what would be declared
ctx-optimize init --scan --yes    # write the FULL found list to config.json
ctx-optimize add .                # fan-out gather: one worker per module
```

### What `scan` refuses to call a module

Generated and vendored trees are never descended into, because a `package.json`
in one of them is noise rather than a project: `.git`, `node_modules`, `vendor`,
`dist`, `build`, `target`, `.venv`, `.next`, `__pycache__`, `.gradle`, `.idea`,
plus **`third_party`** and **`out`** — matched by directory *name* at any depth,
so a nested `net/third_party/…` is pruned like a top-level one.

`third_party` and `out` were added after onboarding chromium reported **241
modules, 217 of them (90%) under `third_party/`** and one being `out/Default`,
the GN build output. With them pruned: **21**.

This is scan-only — the code producer still walks those trees, so nothing stops
being indexed. What changes is that a vendored subtree no longer gets its own
store and its own line in the module list. (Whether vendored code should be
*indexed* at all is a separate open question: chromium's residual is 3.6M nodes.)

Add your own via `scan.markers` / `scan.exclude` in `config.json`, and remember
the module list is hand-editable after `init --scan` — six Cargo fixture dirs
survive the prune on chromium, and deleting them from `config.json` is the
intended fix rather than a cleverer heuristic.

### Progress while a fan-out runs

Ticks stream to stderr — a line when each module **starts**, and one when it
finishes:

```
gathering 17 modules (jobs=8)…
  → infra/postgresbackup
  → tests/api-e2e
[1/17] infra/postgresbackup
[2/17] tests/api-e2e
...
```

If a task outlives 15 seconds a heartbeat names what is still going, so a large
module (or the root residual) is never silent:

```
  … still running (16/17 done): . (2m14s)
```

Detailed per-module results stay ordered on stdout, so `--jobs` never changes
what stdout looks like.

The detailed per-module results print to stdout in a deterministic order
once all workers finish — so piping stdout to a file stays clean.

`scan` finds project markers (package.json, go.mod, pom.xml, pyproject.toml,
csproj/sln, …) to a depth bound (`--depth N`). The generated `modules[]` list
in `.ctxoptimize/config.json` is **yours after generation** — edit, add,
prune, use globs. Commit it.

```json
{
  "name": "acme",
  "modules": [
    {"path": "services/api"},
    {"path": "services/worker"},
    {"path": "apps/*"}
  ]
}
```

## What you get on disk

```
~/ctxoptimize/acme/
  services/api/      # one full store per module (graph, wiki, cards)
  services/worker/
  graph/             # the ROOT RESIDUAL: top-level files not in any module
  navigator.md       # the federation index
  wiki/
```

Modules may nest (a module inside another's tree) — every gather excludes
the other declared dirs inside its own tree, so no file is extracted twice.
A module whose folders are scattered (`{"name": "sdk", "paths": [...]}`)
gathers all of them into ONE store with repo-root-relative IDs.

## Asking questions

- **Inside a module dir**: answers come from that module's store. Zero hits
  escalate across the repo with a note.
- **At the root**: the navigator ranks which modules likely hold the answer
  and federates. `--modules all` or `--modules api,worker` widens/narrows;
  `--root` forces the residual store only.
- `card` / `affected` / `path` note module boundaries instead of silently
  mixing same-named symbols from different modules.

## How reconciliation works (v0.6+)

`config.json` is the contract; `up` reconciles reality against it:

- **Module added to config** (committed or not): its store is missing →
  the next `up` gathers exactly it — nothing else is touched.
- **Broken or deleted store** (even the root residual): `up` re-gathers
  only what's missing — never a full rebuild while healthy module stores
  sit on disk.
- **Module removed from config**: its store becomes an orphan — `add`
  reports it (never searched, safe to delete), and never auto-deletes.
- **The root residual is exempt from the shrink guard**: its scope follows
  the module list, so shrinking massively after you declare modules is
  CORRECT and no longer refused. Module stores keep the guard — a >50%
  drop there still means a broken gather until you pass `--force`.

## Sharing a monorepo store

`remote push`/`pull` at the root move the whole store tree; inside a module
dir the transport script also receives `CTX_SCOPE_PREFIX` so a scope-aware
script can move just that module. See [remote & GitHub](remote-github.md).

## Combined views

```sh
ctx-optimize merge api worker billing --into everything
```

Merged stores are **derived** — re-derive after a pull, never sync them.

Module arguments take the module DIR path (resolved like every verb) or the
store-relative key (`<root>/services/api`).
