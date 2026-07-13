# Sync — share the store, keep it fresh

## Store sync (the team's gathered world)

`remote push` / `pull` against the committed remote in
`.ctxoptimize/config.json` (`remote init <url>` writes it; commit it —
teammates clone and bare `pull` just works). Scope-aware:

- at a multi-module ROOT: the whole store tree (every module + navigator)
- inside a MODULE dir: only that module's prefix (a teammate pulls just the
  module they work on — KBs, sub-second)
- single-module repo: unchanged classic behavior

Sync is incremental (content-hash manifest). Queries never touch the
remote. Merged stores are derived — never synced. Credentials: `${VAR}`
placeholders in config resolve from env at sync time; commit variable
NAMES, never values.

## Freshness (is the store telling the truth?)

- `ctx-optimize status` shows a `fresh:` line — store vs the repo's current
  git HEAD.
- `ctx-optimize fresh` exits 0 fresh / 1 stale / 2 unknown — gate on it in
  automation before trusting the store instead of grep.
- Stale? `ctx-optimize add .` re-gathers (prunes deleted, re-emits changed;
  adapters re-run too, so external content refreshes in the same pass).
