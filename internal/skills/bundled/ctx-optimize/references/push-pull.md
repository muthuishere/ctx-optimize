# Push / pull — share the store with the team

Trigger words from the user: "share the store/graph", "publish it", "push",
"pull", "export to the team", "import/load the teammate's store", "get the
store on my machine". All of them land here.

## One-time setup

`ctx-optimize remote init <s3://bucket/prefix | file:///dir>` — writes the
remote into the repo's committed `.ctxoptimize/config.json`. Commit it:
teammates clone and a bare `remote pull` just works. `--local` keeps the
remote per-machine (store config) instead.

Credentials: `${VAR}` placeholders in config resolve from env at sync time —
commit variable NAMES, never values. S3-compatible anything works (AWS, R2,
MinIO, Hetzner) via the `endpoint` credential key.

## Push (publish your gathered world)

`ctx-optimize remote push` — scope-aware:
- multi-module ROOT → the whole store tree: every module store + the
  navigator (a `stores.json` index is written last, so readers never see a
  half-published tree)
- inside a MODULE dir → only that module's prefix
- single-module repo → the classic whole-store push

## Pull (load someone else's)

`ctx-optimize remote pull` — same scoping. The killer path: a teammate on a
fresh clone cds into the ONE module they work on and pulls just that prefix
— KBs, sub-second — then queries immediately. Pulling at the root fans in
everything.

## Rules

- Transfer is incremental (content-hash manifest); repeat pushes move only
  what changed.
- Queries NEVER touch the remote — pull first, answer from disk.
- Merged stores are derived, never synced; re-derive with `merge` after pull.
- `export --format json|dot|graphml|csv|obsidian` is a different thing —
  that's dumping the graph for OTHER TOOLS, not team sharing.
