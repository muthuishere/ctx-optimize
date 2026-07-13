# Sync & share

Two verbs, two meanings — keep them straight:

## Sync = keep the graph matching the code

The code changed (commits, pulls, refactors) → the graph must follow:

- `ctx-optimize add .` IS the sync: incremental re-gather — prunes deleted
  sources, re-emits changed ones, re-runs every adapter script, refreshes
  the wiki and (multi-module) the navigator. Run it after meaningful code
  changes; it takes seconds even on huge repos.
- `ctx-optimize status` shows a `fresh:` line (store vs the repo's current
  git HEAD). `ctx-optimize fresh` exits 0 fresh / 1 stale / 2 unknown —
  gate on it in automation: stale → `add .` first, then answer.
- Inside a module dir, `add` re-gathers just that module and refreshes its
  navigator entry — you never pay for the whole monorepo to sync one module.

## Share = remote push / pull (the team's store)

Sharing, publishing, exporting to teammates, importing/loading a teammate's
store — that whole lane lives in `./push-pull.md`. Short version:
`remote push` / `remote pull`, scope-aware (root = whole tree, module dir =
just that prefix), config-driven, incremental.
