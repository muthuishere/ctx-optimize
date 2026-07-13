# Sync — keep imported content and the shared store fresh

Two different syncs; don't conflate them.

## 1. Content sync (re-fetch imported docs)

Make the fetch repeatable, then it refreshes on every `add .`:

- Save the working fetch+convert commands as
  `.ctxoptimize/adapters/fetch-<source>.sh` (or .py/.js). An adapter may
  either (a) print a batch to stdout, or (b) refresh files under
  `docs/imported/…` and print an EMPTY batch
  (`{"producer":"fetch-<source>","nodes":[],"edges":[]}`) — the markdown
  producer then re-extracts the refreshed files in the same add run.
- The script reads the provenance frontmatter (source ids, `fetched_at`) to
  know what to re-download; credentials come from broker CLIs (`apl`) or
  env-var NAMES — never values in the script.
- One-off refresh without scripts: repeat the ingest flow by hand; the
  stable file paths make it an overwrite, and `add .` prunes what
  disappeared (producer-scoped Replace).

## 2. Store sync (share the gathered world with the team)

`remote push` / `pull` against the committed remote in
`.ctxoptimize/config.json` — scope-aware:

- at a multi-module ROOT: the whole store tree (every module + navigator)
- inside a MODULE dir: only that module's prefix (a teammate pulls just the
  module they work on — KBs, sub-second)
- single-module repo: unchanged classic behavior

Queries never touch the remote. Merged stores are derived — never synced.

## Freshness

`ctx-optimize status` shows a `fresh:` line (store vs git HEAD);
`ctx-optimize fresh` exits 0 fresh / 1 stale / 2 unknown — gate on it before
trusting the store in automation. Imported-doc staleness is the fetch
script's job (compare `fetched_at` / source etag when the source offers one).
