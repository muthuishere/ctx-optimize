# Cookbook — 32 scenarios, every one executed and verified

Every scenario below was run against the real binary (v0.6.0 dev, 2026-07-19)
by the scenario matrix before landing in this document. Commands are exactly
what ran; outputs are excerpts of what actually printed. Grouped by
situation — find yours.

---

## Daily life, single repo

### S01 · Brand-new repo → answering questions in one command

```sh
$ cd my-repo && ctx-optimize up
no config — bootstrapping: ...
$ ctx-optimize query "refund flow"
Refund flow  [section]  README.md L3-L6
ProcessRefund  [function]  pay.go L3-L3
```

### S02 · Run `up` again anytime — it no-ops when fresh

```
$ ctx-optimize up
up: store ready — up to date with git HEAD
```

### S03 · You committed code → store is stale → `up` refreshes

```
$ ctx-optimize fresh; echo $?     # 1 = stale
$ ctx-optimize up                 # fast re-gather
$ ctx-optimize fresh; echo $?     # 0 = fresh again
```

### S04 · Not a git repo? Still works

Gather and query work in any folder; freshness reports `unknown`
(`fresh` exits 2) because there's no HEAD to compare against.

### S05 · The intent verbs

```
$ ctx-optimize card ProcessRefund        # signature + body head + calls, no file read
  calls (1):
    pay.go::validateRefund
$ ctx-optimize change-plan validateRefund
callers (1):
  pay.go::ProcessRefund
blast radius (depth 2, 2 shown): ...
```

### S06 · Trust but verify a citation

```
$ ctx-optimize verify "pay.go:L1-L5";    echo $?   # 0 — holds
$ ctx-optimize verify "pay.go:L900-L950"; echo $?  # 1 — out of bounds, refused
```

### S29–S32 · Export, audit, learning loop, wiki

```
$ ctx-optimize export --format dot --out g.dot     # digraph for graphviz
$ ctx-optimize log | tail -1                       # every mutation audited
$ ctx-optimize save-result --question "where is refund" --type query --outcome useful
$ ctx-optimize reflect                             # → reflections/LESSONS.md
```

The store's `wiki/` regenerates on every add and names your symbols.

---

## Guards firing (see [troubleshooting](troubleshooting.md) for all of them)

### S07 · Real mass deletion → guard refuses → `--force` applies

```
$ ctx-optimize add .
ctx-optimize: refusing to shrink producer "code" from 13 to 4 nodes — pass --force if this is a real deletion
$ ctx-optimize add . --force      # you confirmed: it applies
```

### S08 · Two repos named `app` → give one its own store name

```sh
# .ctxoptimize/config.json:  {"name": "app-b"}
$ ctx-optimize add .
added 1 nodes → ~/ctxoptimize/app-b
```

(Note: `"name"` is edited in config.json directly — it is not a
`ctx-optimize config` key.)

### S09 · Committed `.env` → loud warning, exact command

```
WARNING: .env is TRACKED in git — secrets in it are already exposed; untrack it: git rm --cached .env
```

---

## Native sources

### S10 · Literal password in committed config → hard refusal at load

```
$ ctx-optimize status
ctx-optimize: .ctxoptimize/config.json sources[0]: ... credentials belong in env ...
```

### S11 · Teammate without the credential → clean skip, CI can be strict

```
$ ctx-optimize up --sources=always
source TEAM_ONLY_DB_URL: skipped — TEAM_ONLY_DB_URL not set (checked env, .env, ~/.config/ctx-optimize/.env)
$ ctx-optimize up --sources=always --strict; echo $?   # 1 — CI fails instead
```

### S12 · OpenAPI spec by file path, credential-free, via the machine-global env

```sh
$ echo 'PETS_SPEC=spec.json' >> ~/.config/ctx-optimize/.env
$ ctx-optimize add PETS_SPEC
source PETS_SPEC ← ~/.config/ctx-optimize/.env: captured (3 nodes, 2 edges)
$ ctx-optimize query "listPets"
GET /pets  [operation]  spec.json
```

### S13 · Debug a source without touching the store

```
$ ctx-optimize capture PETS_SPEC     # batch JSON on stdout, nothing written
{"producer":"source:PETS_SPEC", ...}
```

---

## Custom adapters

### S14 · Drop a script — the file IS the registration

```sh
$ cat > .ctxoptimize/adapters/tickets.sh <<'EOF'
#!/bin/sh
echo '{"producer":"tickets","nodes":[{"id":"t:1","label":"TCK-1 checkout bug","kind":"ticket","file_type":"external","source":"tickets"}]}'
EOF
$ ctx-optimize add .
adapter tickets: 1 nodes, 0 edges
$ ctx-optimize query "TCK-1"
TCK-1 checkout bug  [ticket]  tickets
```

### S15 · `sync` skips adapters; their nodes stay put

Fast inner-loop refresh never re-runs slow scripts — producer-scoped
merges keep adapter nodes intact until `adapters run`.

### S16 · Pipe any batch through the `--json` door

```
$ my-exporter | ctx-optimize add --json -
```

### S17 · Broken batch → fail-closed, names the defect

```
$ echo '{"nodes":[{"id":"x"}]}' | ctx-optimize add --json -
ctx-optimize: reject batch: batch: producer is required (provenance tag)
```

---

## Monorepos

### S18 · Onboard: preview → declare → fan-out → navigator

```
$ ctx-optimize scan            # read-only: what would be declared
$ ctx-optimize init --scan --yes && ctx-optimize add .
== services/api ... == services/worker ... == navigator
2 modules → ~/ctxoptimize/mono/navigator.md
```

### S19/S20 · Scope follows cwd; misses escalate

Inside `services/api`, queries answer from api's store; asking api for a
worker symbol escalates repo-wide and still finds it, labeled with its
module.

### S21 · Add a module to config.json (no commit needed) → `up` gathers exactly it

```
$ ctx-optimize up
up: 1 of 4 declared stores missing — gathering only those:
== services/billing
```

### S22 · Broken root store → `up` repairs only it — never a full rebuild

```
up: 1 of 4 declared stores missing — gathering only those:
== .
```

### S23 · Splitting a whole-tree fossil into modules just works

The root residual is exempt from the shrink guard (its scope follows the
module list) — the 200k→3k volentis case now converges with zero `--force`.

### S24 · Remove a module from config → orphan reported, never deleted

```
note: 1 module store(s) on disk are no longer in config.json — never searched, safe to delete under ~/ctxoptimize/mono: services/billing
```

### S25 · KNOWN LIMITATION — `merge` can't reach nested module stores

`merge` addresses top-level store keys only (v0.6.0). Tracked in
`openspec/changes/2026-07-19-merge-nested-module-keys/`; root federation
covers the combined view meanwhile.

---

## Team sharing

### S26 · GitHub/git remote round-trip, end to end

Verified against a local bare repo (the transport is identical):

```
$ ctx-optimize remote push
pushed s26 -> ~/ctx-stores -> <store host>.git
$ rm -rf ~/ctxoptimize/s26          # simulate a fresh machine
$ ctx-optimize up
up: store ready (pulled)
$ ctx-optimize query "refund flow"  # answers from the pulled store
```

Setup recipe: [remote-github.md](remote-github.md).

---

## Agent surface

### S27 · Pointer blocks respect your repo's layout

A repo with only `AGENTS.md` gets the block there — no `CLAUDE.md` is
created (verified: file absent after init). Neither file exists → both are
created. `--instructions NONE` opts out.

### S28 · Your notes in instructions.md survive every re-init

Text outside the managed markers is never touched — add team notes freely.

---

## Reproduce all of this

```sh
bash proof/scenarios/run.sh     # runs every scenario against your installed binary
```

All 33 checks ran green on 2026-07-19 against v0.6.0. If a doc example
ever drifts from real output, that's a bug — file it.
