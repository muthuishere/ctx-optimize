# Sharing the store — GitHub as the remote (and any other transport)

## Why share at all

One person (or CI) gathers; everyone else pulls a prebuilt graph in seconds
instead of re-gathering. A teammate's flow on a fresh machine is exactly:

```sh
git clone <your-repo> && cd <your-repo>
ctx-optimize up        # runs the declared pull; falls back to gathering, loudly
```

## The model: the remote is YOUR script

The binary ships **no transport**. `remote push` / `remote pull` run the
commands you declare in the committed `.ctxoptimize/config.json`:

```json
{
  "remote": {
    "push": "node .ctxoptimize/push.js",
    "pull": "node .ctxoptimize/pull.js"
  }
}
```

Any shell line works — node, python, sh, or inline. Your script receives:

| Env var | Meaning |
|---|---|
| `CTX_STORE_DIR` | local store tree (push: source · pull: destination, pre-created) |
| `CTX_STORE_KEY` | the store's key under `~/ctxoptimize/` |
| `CTX_SCOPE_PREFIX` | module store-key segment when run inside a module, else empty |
| `CTX_DIRECTION` | `push` or `pull` — one script can serve both |

cwd = repo root; non-zero exit fails the verb; stdout/stderr stream through.
**Secrets: env-var NAMES only** in scripts and config — the shell expands
them at run time; never hardcode or print values.

## Lane A — GitHub repo as the store host (recommended)

Only needs git. Store artifacts are sorted ndjson, so git diffs and merges
them cleanly. `init` already scaffolded a complete git lane as inert
samples — arming them is usually all a team needs:

```sh
# once per team: a private repo to hold store trees
gh repo create <org>/ctx-stores --private

# in your code repo: arm the scaffolded samples
mv .ctxoptimize/push.js.sample .ctxoptimize/push.js
mv .ctxoptimize/pull.js.sample .ctxoptimize/pull.js
# edit both: set STORE_REPO_URL to git@github.com:<org>/ctx-stores.git
# declare them in config.json (the "remote" block above)
git add .ctxoptimize && git commit -m "share the ctx store over github"

ctx-optimize add .          # gather
ctx-optimize remote push    # publish
```

The scripts clone/pull `~/ctx-stores`, copy the store tree in/out under the
store key, and commit+push. Auth is whatever your git already does (ssh
keys, gh auth) — nothing new to manage.

**CI refresh** (optional): a job that runs `ctx-optimize up && ctx-optimize
remote push` on main keeps the shared store current so humans never gather.

## Lane B — S3-compatible bucket (AWS, R2, MinIO)

One script, both directions — save as `.ctxoptimize/s3sync.js`, declare it
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

Credentials via standard `AWS_*` env vars / profiles; non-AWS endpoints
(R2, MinIO): set `AWS_ENDPOINT_URL` in the environment.

## Lane C — anything else

GCS, Artifactory, rsync-over-ssh, a network share: author the script that
copies `CTX_STORE_DIR` to/from your host, declare it, commit it. The binary
never cares what the transport is.

## Rules worth knowing

- Queries NEVER touch the remote — pull first, answer from disk.
- Merged stores are derived, never synced; re-derive with `merge` after pull.
- `export` (json/dot/graphml/csv/obsidian) is for OTHER TOOLS, not sharing.
- v0.3's built-in `remote init <url>` / `file://` / `s3://` transports are
  gone since v0.4; a legacy URL-form config loads inert and push/pull
  explain the migration.
