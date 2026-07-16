# Push / pull — share the store with the team

Trigger words from the user: "share the store/graph", "publish it", "push",
"pull", "export to the team", "import/load the teammate's store", "get the
store on my machine", "set up sharing over github / a bucket". All of them
land here.

There are exactly two hosting lanes — a git repo (only needs git/gh) and an
S3-compatible bucket. YOU set either one up end-to-end; the recipes below
are complete. `init` also scaffolds them into the repo as
`.ctxoptimize/remote.example.md` with the store name pre-filled — check for
that file first and follow it if present.

## One-time setup

`ctx-optimize remote init <url>` writes the remote into the repo's committed
`.ctxoptimize/config.json`. Commit it: teammates clone and a bare
`remote pull` just works. `--local` keeps the remote per-machine (store
config) instead.

Credentials: `${VAR}` placeholders in config resolve from env at sync time —
commit variable NAMES, never values, and never print them.

### Lane A — a GitHub repo as the store host (no bucket, no cloud account)

The store syncs to a local folder (`file://`), and that folder is a git
clone the team pushes/pulls like any repo. Set it up for the user:

```
gh repo create <org>/ctx-stores --private          # once per team
gh repo clone <org>/ctx-stores ~/ctx-stores        # once per machine
ctx-optimize remote init "file://$HOME/ctx-stores/<store-name>"   # in the repo; commit config.json
```

Publish after a gather (run both — the second is what teammates see):

```
ctx-optimize remote push
git -C ~/ctx-stores add -A && git -C ~/ctx-stores commit -m "store: <store-name>" && git -C ~/ctx-stores push
```

Teammate on a fresh machine:

```
gh repo clone <org>/ctx-stores ~/ctx-stores
git -C ~/ctx-stores pull                           # refresh on later days
ctx-optimize remote pull                           # inside the code repo
```

Store artifacts are sorted, newline-terminated ndjson — git diffs and merges
them cleanly.

### Lane B — S3-compatible bucket (AWS, R2, MinIO, Hetzner)

```
ctx-optimize remote init "s3://<bucket>/ctx/<store-name>"
```

Plain AWS with `AWS_*` env vars needs nothing more. Non-AWS endpoints or
team-specific keys: write the object form into `.ctxoptimize/config.json` —

```json
"remote": {
  "type": "s3",
  "url": "s3://<bucket>/ctx/<store-name>",
  "credentials": {
    "access_key_id": "${TEAM_KEY_ID}",
    "secret_access_key": "${TEAM_SECRET}",
    "region": "auto",
    "endpoint": "${R2_ENDPOINT}"
  }
}
```

Omitted credential keys fall back to the standard `AWS_*` env vars. The
binary speaks SigV4 itself — no aws-cli, no SDK to install.

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
