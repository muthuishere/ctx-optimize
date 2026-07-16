# Share this store with the team — push/pull recipes (delete me if unused)

`remote push` publishes the gathered store; `remote pull` fetches it. The
remote lives in config.json next to this file — set it once, commit it, and a
teammate's bare `ctx-optimize remote pull` just works. `${VAR}` placeholders
resolve from env at sync time: commit variable NAMES, never secret values.

## Option A — a git repo as the store host (needs only git/gh)

One-time (owner):

    gh repo create your-org/ctx-stores --private
    gh repo clone your-org/ctx-stores ~/ctx-stores
    ctx-optimize remote init "file://${HOME}/ctx-stores/${NAME}"

Publish after a gather:

    ctx-optimize remote push
    git -C ~/ctx-stores add -A && git -C ~/ctx-stores commit -m "store: ${NAME}" && git -C ~/ctx-stores push

Teammate (fresh machine):

    gh repo clone your-org/ctx-stores ~/ctx-stores
    ctx-optimize remote pull        # run inside this repo

## Option B — S3-compatible bucket (AWS, R2, MinIO, Hetzner)

    ctx-optimize remote init "s3://your-bucket/ctx/${NAME}"

or copy this into config.json and edit (credentials optional — omitted keys
fall back to the standard AWS_* env vars):

    "remote": {
      "type": "s3",
      "url": "s3://your-bucket/ctx/${NAME}",
      "credentials": {
        "access_key_id": "${TEAM_KEY_ID}",
        "secret_access_key": "${TEAM_SECRET}",
        "region": "auto",
        "endpoint": "${R2_ENDPOINT}"
      }
    }

Then `ctx-optimize remote push` / `remote pull`. Transfer is incremental
(content-hash manifest); queries never touch the remote — pull first, answer
from disk.
