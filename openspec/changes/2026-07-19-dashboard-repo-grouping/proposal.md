# Dashboard: group module stores under their repo (a monorepo is one product, not 18)

Date: 2026-07-19 · Status: ADOPTED (owner 2026-07-19: "go with a" — inline
expand with a cap)

## Problem — reproduced

`listModules` (internal/dashboard/dashboard.go:396) walks the store root
recursively and appends EVERY dir holding `graph/nodes.ndjson` as a flat
peer. `Repos.tsx:65` renders `stores.map(...)` — one card per store, no
hierarchy. So volentis (17 modules + residual) renders as **18 sibling
cards**, visually equal to genuinely separate repos (ctx-optimize, brain).
The monorepo floods the list and its identity as ONE product is lost.

Owner report: "the serve is making submodules as separate products".

Every other surface disagrees with that presentation: the CLI operates on
the repo, the navigator federates modules, `config.json` declares them as
parts of one thing.

## Decision — group by repo, modules inline, capped

The backend already carries the parent: `StoreInfo.Root` is the top-level
key (`volentis`) alongside `Key` (`volentis/apps/librechat/api`). No API
redesign — the frontend groups on it.

1. **One card per repo** (`Root`). Header shows the product: repo name,
   aggregate nodes/edges summed across its modules + residual, a freshness
   roll-up (worst-of, mirroring `freshness.Overall`: any stale ⇒ stale,
   else any unknown ⇒ unknown, else fresh), summed usage counters, merged
   producer counts.
2. **Modules inline inside the card**, sorted by node count desc, **capped
   at 5 shown** with a `+N more` toggle that expands the rest in place
   (owner: option (a) with a cap). No route change, no extra screen.
3. **The root residual** is an entry INSIDE its repo card, labeled as the
   root/top-level files — never a peer card.
4. **Single-module repos are unchanged**: `Root == Key`, so the group has
   exactly one member and renders as today (no expander).
5. **Overview** store count reads `N repos · M modules` instead of one
   flat number.

## What this does NOT change

- No API/route changes; `/api/stores` keeps returning the flat list (other
  consumers and the CLI parity stay intact).
- No store layout change on disk.
- Read-path only — no new mutations, no new permissions surface.

## Gates

- `dashboard-ui` unit tests: grouping puts N module stores under one repo;
  aggregates sum; freshness roll-up follows worst-of; single-module repo
  yields one ungrouped-looking card; cap shows 5 + expander.
- `task dashboard-build` regenerates the committed dist; `task ci` green
  (go install must never need node — dist stays committed).
- Manual: `ctx-optimize serve` against a store root holding volentis (18
  entries) + several single repos shows one volentis card.
