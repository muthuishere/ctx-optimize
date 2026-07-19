# Troubleshooting — every guard and warning, verbatim, with the one command

ctx-optimize follows one shape everywhere: **detect, say it plainly, name
the one command — never act destructively on its own.** So every blocking
message below is the tool asking a human to confirm something. Find your
message, read the cause, run the command.

---

## `refusing to shrink producer "code" from N to M nodes — pass --force if this is a real deletion`

**What it means**: a re-gather produced less than half the nodes this
producer previously held. That usually means a broken or partial gather
(wrong directory, sparse checkout, store-name collision), not a real
deletion — so the old store is kept intact.

**Check first, in order of likelihood:**

1. **Store-name collision** — the store key is the repo's basename (or
   config `name`). Two different clones sharing a basename overwrite each
   other. `ctx-optimize status` shows which repo the store was built from;
   if it's not yours, give your repo its own name and re-run — do **not**
   `--force` (that would destroy the other repo's store). Set `"name"` in
   `.ctxoptimize/config.json`:
   ```json
   {"name": "my-unique-name"}
   ```
   then `ctx-optimize add .` — the store lands under
   `~/ctxoptimize/my-unique-name/`, leaving the other repo's store alone.
2. **Wrong directory / scope** — you ran `add .` from a subdirectory, or
   the config now covers a slice of what the original gather covered.
3. **A genuinely smaller checkout** — sparse/shallow clone, or a branch
   where most of the tree is absent.

Only when the shrink is real and intended:

```sh
ctx-optimize add . --force
```

**Monorepo root note (v0.6+)**: the root residual store (top-level files
outside every module) is exempt from this guard — declaring modules shrinks
it legitimately and no longer refuses. Module stores keep the guard.

---

## `note: N module store(s) on disk are no longer in config.json — never searched, safe to delete under <dir>: <keys>`

**What it means**: you removed modules from `modules[]`; their old stores
remain on disk as orphans. They are inert — never federated, never
refreshed — but silent leftovers look authoritative, so `add` names them.

**The command** (only if you want the disk back — leaving them is harmless):

```sh
rm -rf ~/ctxoptimize/<root-key>/<module-key>
```

Re-declaring the module later re-gathers fresh; the orphan is never read.

---

## `✗ STALE — store at <sha>, repo now at <sha>; run: ctx-optimize add .`

**What it means**: the code moved past the store's recorded git HEAD.
`up` fixes this automatically (fast re-gather); `status`/`fresh` only
report it.

```sh
ctx-optimize up        # or: ctx-optimize add .
```

## `fresh` exit codes (for hooks and CI)

| Exit | Meaning | Do |
|---|---|---|
| 0 | store current with git HEAD | trust answers |
| 1 | stale — code moved past the store | `ctx-optimize up` |
| 2 | unknown — no git provenance (non-git dir, moved repo) | `ctx-optimize sync` to force a refresh if in doubt |

---

## `WARNING: .env is TRACKED in git — secrets in it are already exposed; untrack it: git rm --cached .env`

**What it means**: your repo-root `.env` (a rung of the source-credential
ladder) is committed. A gitignore added later does NOT untrack an
already-committed file — the index wins.

```sh
git rm --cached .env && git commit -m "untrack .env"
```

Then rotate anything that was in it — it's in history.

---

## `legacy remote config (v0.3 URL form) — transports are scripts now`

**What it means**: your committed config still has the retired
`"remote": {"type": "s3", "url": ...}` (or plain URL) form. It loads
inert — push/pull do nothing. Since v0.4 the remote is a command you
declare. Migration is two lines plus a script — see
[remote-github.md](remote-github.md):

```json
"remote": {"push": "node .ctxoptimize/push.js", "pull": "node .ctxoptimize/pull.js"}
```

---

## `source capture needs the ctx-optimize-adapters companion (installed beside ctx-optimize by npm/releases)`

**What it means**: native-source capture (postgres/kafka/s3/…) runs in a
second binary shipped beside the main one; it's missing — usually a manual
copy of just one binary onto PATH.

```sh
npm install -g @muthuishere/ctx-optimize     # reinstall lands both
```

or download `ctx-optimize-adapters` from the GitHub release archive into
the same directory as `ctx-optimize`.

## `the ctx-optimize-adapters companion takes env-var names only — put the full URL in a single env var`

**What it means**: a URL *template* with `$VARS` spread across the entry
can't cross the bridge (names-only argv, by design). Fold it:

```sh
export MY_DB_URL='postgres://$PG_USER:$PG_PASS@db.internal:5432/app'
ctx-optimize add MY_DB_URL
```

---

## `source X ← .env: FAILED (…)` / `sources: N captured, M skipped, K failed`

- **skipped** = the env var is unset on this machine — normal for
  teammates without credentials; prior nodes stay. `--strict` makes CI
  fail instead.
- **failed** = the dial itself broke. Debug without touching the store:
  ```sh
  ctx-optimize capture MY_DB_URL     # full (scrubbed) error + batch JSON
  ```

## Sources look stale

Recorded sources refresh on `up` under a 24h TTL. Force it:

```sh
ctx-optimize up --sources=always
```

---

## A query cites a line that doesn't match the file

The store is a snapshot. Before acting on any citation:

```sh
ctx-optimize verify "internal/app/app.go:L100-L120"
```

Exit 0 = node exists, file exists, range in bounds, no drift since gather.
Anything else → `ctx-optimize up`, then re-ask.
