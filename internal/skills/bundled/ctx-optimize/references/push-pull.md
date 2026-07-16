# Push / pull — share the store; the remote is a script YOU author

Trigger words from the user: "share the store/graph", "publish it", "push",
"pull", "export to the team", "import/load the teammate's store", "get the
store on my machine", "set up sharing over github / a bucket / rsync". All
of them land here.

The binary ships NO transport. `ctx-optimize remote push` / `remote pull`
run the commands declared in the repo's committed `.ctxoptimize/config.json`:

```json
"remote": {
  "push": "node .ctxoptimize/push.js",
  "pull": "node .ctxoptimize/pull.js"
}
```

Any shell line works — `node …`, `python3 …`, `sh …`, or inline. YOUR JOB on
"set up sharing" is to AUTHOR the transport (write the script, add the two
config lines, commit both) — not to recite steps into chat. `init` scaffolds
`push.js.sample` + `pull.js.sample` (a complete git lane — arming them is
often all a team needs) and `remote.example.md`; prefer arming those samples
over writing from scratch.

## The contract your script gets

The binary resolves scope, then runs the command (cwd = repo root) with:

```
CTX_STORE_DIR     local store tree (push: source · pull: destination, pre-created)
CTX_STORE_KEY     the store's key under ~/ctxoptimize/
CTX_SCOPE_PREFIX  module store-key segment when run inside a module, else empty
CTX_DIRECTION     "push" or "pull" — one script can serve both commands
```

Exit non-zero fails the verb; stdout/stderr stream to the user. Secrets:
env-var NAMES only in scripts and config — the shell expands them at run
time; never hardcode or print values.

## Lane A — git repo as the store host (recommended; only needs git)

Arm the scaffolded samples:

```
gh repo create <org>/ctx-stores --private        # once per team
mv .ctxoptimize/push.js.sample .ctxoptimize/push.js
mv .ctxoptimize/pull.js.sample .ctxoptimize/pull.js
# set STORE_REPO_URL in both files, add the "remote" block to config.json, commit
```

The scripts clone/pull `~/ctx-stores`, copy the store tree in/out, and
commit+push. Store artifacts are sorted ndjson — git diffs and merges them
cleanly. Teammate on a fresh machine: clone the code repo, `ctx-optimize up`
(runs the declared pull; falls back to a local gather).

## Lane B — S3-compatible bucket (AWS, R2, MinIO — needs the aws CLI)

One script, both directions, save as `.ctxoptimize/s3sync.js` and declare it
for push AND pull:

```js
#!/usr/bin/env node
const { execFileSync } = require("node:child_process");
const S3_URL = "s3://your-bucket/ctx";  // store key is appended
const dir = process.env.CTX_STORE_DIR, key = process.env.CTX_STORE_KEY;
const remote = S3_URL + "/" + key;
const [from, to] = process.env.CTX_DIRECTION === "push" ? [dir, remote] : [remote, dir];
execFileSync("aws", ["s3", "sync", from, to, "--delete"], { stdio: "inherit" });
```

Credentials via the standard `AWS_*` env vars / profiles; non-AWS endpoints
(R2, MinIO): `AWS_ENDPOINT_URL` in the environment.

## Lane C — anything else (GCS, artifactory, rsync-over-ssh)

Author the script that copies `CTX_STORE_DIR` to/from the host, declare it,
commit it. The binary never cares what the transport is.

## Scope

`remote push`/`remote pull` resolve scope like every verb: at a multi-module
root the script gets the whole store tree; inside a module dir it ALSO gets
`CTX_SCOPE_PREFIX` so a scope-aware script can move just that prefix (the
scaffolded samples move the whole tree — fine for KB–MB stores).

## Rules

- v0.3's `remote init <url>` and the built-in file://+s3:// transports are
  GONE (v0.4). A legacy URL-form config loads but push/pull explain the
  migration. Never re-create `remote init` — declare commands instead.
- Queries NEVER touch the remote — pull first, answer from disk.
- Merged stores are derived, never synced; re-derive with `merge` after pull.
- `export --format json|dot|graphml|csv|obsidian` is a different thing —
  that's dumping the graph for OTHER TOOLS, not team sharing.
