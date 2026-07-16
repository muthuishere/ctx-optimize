# ADR — remote transports become COMMITTED SCRIPTS; the skill is the generator; `remote init` retired

Status: DRAFT v2 — shape agreed with maintainer 2026-07-16 (v1's generator
verb dropped after discussion: "do we need remote init at all? we already
have config.json"). Maintainer steers, verbatim, same session:

- "we definitely need that" — push.js / pull.js transport scripts
- "no make js or python or sh, put in config.json" — commands DECLARED in
  config.json, not filename-discovered
- "it will be in config.json right: remote -> push, remote -> pull, some js
  or py or direct"
- "i dont want remote direct at all … just scripts" — the binary ships NO
  transport of its own
- "ctx-optimize remote push and remote pull — all are like it" — the verb
  spelling stays
- "do we need remote init at all…" → no; config.json is the single source
- "your skill should be updated" — the skill IS the generator; updating it
  is in scope, not a follow-up

## Context

v0.3.x ships two built-in transports (`internal/remote`: `file://` +
`s3://` stdlib SigV4) selected by a URL that `remote init <url>` writes
into config.json. That bakes transport policy into the binary — every new
host (GCS, artifactory, rsync-over-ssh) is a driver request, the exact door
the doctrine closes everywhere else: documents/DB/messaging enter via
SCRIPTS through the validated `--json` door; the binary interprets nothing.
Sharing should work the same way. And once transports are scripts, an init
verb has no job left — codegen belongs to the host agent (the skill), not
the deterministic binary.

## Decision 1 — verbs: `remote push` / `remote pull` only

- Spelling unchanged; scope-aware as today (multi-module root = whole
  tree; module dir = its prefix, exposed to the script as env).
- `remote init` is REMOVED. Running any retired form errors with the
  migration shape: declare `remote.push`/`remote.pull` in config.json
  (recipes in `.ctxoptimize/remote.example.md`).

## Decision 2 — config.json is the single source (no second config file)

```json
"remote": {
  "push": "node .ctxoptimize/push.js",
  "pull": "node .ctxoptimize/pull.js"
}
```

- One committed `.ctxoptimize/config.json`, as today — no
  `remote-config.json`; remotes are team-shared by design, and the
  per-machine case (`remote init --local` + store-local config.json) is
  retired with it.
- Each value is a shell line — "some js or py or direct": `node …`,
  `python3 …`, `sh …`, or inline (`rsync -a … && git -C … push`). Same
  trust model as `adapters[].run` and npm scripts.
- Legacy v0.3 forms (`"remote": "s3://…"` string, `{type,url,credentials}`
  object) still PARSE (an old committed config never breaks Load) but are
  inert: push/pull report "legacy remote config — declare push/pull
  commands". They are never written again.

## Decision 3 — the binary only RUNS the declared command

- `remote push`/`remote pull`: load config at the scope root, run the
  declared line via the shell (cwd = repo root; `cmd /c` on windows, as
  adapters). Missing declaration → error naming the config shape and the
  recipe file. Exit != 0 fails the verb; the command's output streams
  through.
- Env contract handed to every transport command:

```
CTX_STORE_DIR     absolute path of the local store tree (push: source; pull: destination — pre-created)
CTX_STORE_KEY     the store's key under the store root
CTX_SCOPE_PREFIX  module store-key segment when invoked inside a module, else ""
CTX_DIRECTION     "push" or "pull" (one script can serve both)
```

- `init`'s auto-pull-on-clone keeps working: committed config declares
  `remote.pull`, local store is empty → init runs the pull command, falls
  back to a printed hint on failure.
- `status` reports which commands are declared (never echoes secrets — the
  command strings are committed text already).

## Decision 4 — built-in transports RETIRED ("just scripts")

- `internal/remote` (file:// + s3:// SigV4, tree sync, manifest-diff
  transfer) is DELETED, with its tests. The binary performs no sync network
  I/O of its own — network remains user-invoked only (`update`,
  `grammar build`), and sync bytes now move through the user's script.
- Incrementality becomes the script's property: git/rsync lanes are
  naturally incremental; a naive `aws s3 cp --recursive` is not. Accepted —
  stores are KBs–MBs and the git lane is the default recommendation.

## Decision 5 — the SKILL is the generator (first-class scope)

The agent, not the binary, authors transports. Where generation could have
lived, and why here (maintainer 2026-07-16: "not in binary — can we use in
skill … because push/pull is js only right"):

- **Go binary** (`remote create git|s3`) — REJECTED: codegen in the
  deterministic binary means embedded js templates, flags and prompt UX to
  maintain, for something an agent tailors better per team.
- **npm wrapper** (a js generator bin alongside the launcher) — REJECTED:
  forks the surface (npm installs get it, binary downloads don't), adds a
  second CLI to version and test; the wrapper stays a thin launcher.
- **Skill + scaffolded samples** — CHOSEN: the transports are js files, so
  their generator is the agent writing js — its native move. Skill-less
  humans are covered by `init`'s inert samples (rename + two config lines).

Skill updates ship in the same change:

- **push-pull.md** — rewritten: on "set up sharing", the agent WRITES
  `.ctxoptimize/push.js` + `pull.js` (or py/sh — team's choice) from the
  lane recipes and adds the two config lines, then commits. Recipes
  carried: git lane (gh repo as store host — create/clone, rsync into the
  clone, add/commit/push; pull mirrors), s3 lane (aws CLI or curl SigV4 —
  the agent picks what the team has), custom lane (env contract + rules).
- **SKILL.md** — share row routes "set up sharing / push / pull" to the
  authoring flow; frontmatter keeps the share trigger words.
- **activation-routing.xml** — `remote-init` route replaced by a
  `remote-author` route (when: set up sharing → goal: write the scripts +
  config lines); `remote-sync` route updated (no url, config is the source,
  env contract named).
- **Scaffold** (`ctx-optimize init`) ships inert samples next to the
  adapter sample: `push.js.sample` / `pull.js.sample` (git lane, zero-dep
  node) + `remote.example.md` rewritten around authoring — so a skill-less
  human arms sharing by renaming two files and adding two config lines.
- Secrets rule unchanged everywhere: env-var NAMES only, in scripts and
  config alike; scripts must never print values.

## Decision 6 — breaking release v0.4.0

- First intentionally breaking minor. CHANGELOG migration table:
  `remote init <url>` → declare `remote.push`/`remote.pull` (+ scripts);
  s3 users regenerate their lane as a script; `--local` users move the
  commands into the committed config.
- Judged acceptable this early: v0.3 is days old and the install base ≈
  the team.

## Consequences

- The binary loses ~700 lines of transport + the S3 test surface; sharing
  becomes auditable repo content. The doctrine gets simpler to state: the
  binary NEVER moves bytes to a host it chose — every transport is a
  committed script the team can read.
- The skill grows real authoring responsibility (script generation), which
  is where per-team variation (host, language, credential names) belongs.
- Windows: generated js runs via node (already required for js adapters);
  the command line runs through `cmd /c` as adapters do today.
