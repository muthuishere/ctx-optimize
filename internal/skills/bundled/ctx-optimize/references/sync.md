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

`remote push` / `pull` against the committed remote in
`.ctxoptimize/config.json` (`remote init <url>` writes it; commit it —
teammates clone and bare `pull` just works). Scope-aware:

- at a multi-module ROOT: the whole store tree (every module + navigator)
- inside a MODULE dir: only that module's prefix (a teammate pulls just the
  module they work on — KBs, sub-second)
- single-module repo: unchanged classic behavior

Transfer is incremental (content-hash manifest). Queries never touch the
remote. Merged stores are derived — never synced. Credentials: `${VAR}`
placeholders in config resolve from env at sync time; commit variable
NAMES, never values.
