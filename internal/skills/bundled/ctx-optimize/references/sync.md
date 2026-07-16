# Sync & share

Two verbs, two meanings — keep them straight:

## Sync = keep the graph matching the code

The code changed (commits, pulls, refactors) → the graph must follow. Two
lanes, split by speed:

- `ctx-optimize sync` is the FAST lane, the default after code changes:
  incremental re-gather of the repo you're in (code, docs, manifests, git —
  prunes deleted sources, re-emits changed ones, refreshes the wiki and the
  navigator) but SKIPS adapter scripts, which can be arbitrarily slow (DB
  dumps, doc converters). Skipping is safe: replace is producer-scoped, so
  adapter nodes stay put. Takes no path — it always syncs the repo you're
  in (`add <path>` for another repo). Prints how many adapters it skipped.
- `ctx-optimize adapters run [name]` is the SLOW lane, on demand: re-run
  every adapter script, or just one by name (`adapters list` shows what
  exists). Run it when the external system changed — the schema migrated,
  the topics moved — not on every code edit.
- `ctx-optimize add .` is the FULL gather: both lanes in one pass
  (built-ins + every adapter). Same as `sync` when no adapters exist.
  `add . --no-adapters` ≡ `sync`.
- `ctx-optimize status` shows a `fresh:` line (store vs the repo's current
  git HEAD). `ctx-optimize fresh` exits 0 fresh / 1 stale / 2 unknown —
  gate on it in automation: stale → `sync` first, then answer.
- Inside a module dir, `sync`/`add` re-gathers just that module and refreshes
  its navigator entry — you never pay for the whole monorepo to sync one
  module.

## Share = remote push / pull (the team's store)

Sharing, publishing, exporting to teammates, importing/loading a teammate's
store — that whole lane lives in `./push-pull.md`. Short version:
`remote push` / `remote pull` run the transport commands declared in
config.json (the remote is the team's committed script), scope-aware (root = whole tree, module dir =
just that prefix), config-driven, incremental.
