# config.json is the reconciliation contract — root residual, shrink guard, and `up` all answer to it

Date: 2026-07-19 · Status: DRAFT — under discussion, no code until owner agrees
Supersedes: 2026-07-19-config-aware-freshness (snapshot-hash comparison —
reverted unmerged; reconciliation against the present config dissolves the
problem it chased)

## Problem — one real session, four failures

Owner stress-tested the volentis monorepo (14–19 modules, module list edited
between runs) on 2026-07-19. Full transcript in the session log; every
failure reproduced against a dev build at f80d54e:

1. **Shrink guard vs residual, structurally.** With modules declared, the
   root `.` gathers the RESIDUAL (top-level tree minus module subtrees —
   multimodule.go:411). The residual's size is a function of the module
   list: volentis' root went 206717 (pre-split fossil) → 3018 (18 modules)
   → 161 (15 modules). Each re-gather trips `refusing to shrink producer
   "code"` — the guard compares raw counts and cannot know the scope
   legitimately shrank. `--force` clears it once; the next module-list edit
   re-arms it. Structural, not a one-off fossil.
2. **`up` reports "up to date" right after a failed gather.** `add .`
   failed on the root module (`1 of 19 modules failed`); the very next
   `up` printed `store ready — up to date with git HEAD`. Freshness only
   compares recorded git HEAD; "did the last gather complete" is invisible
   to it.
3. **A broken/empty root makes `up` rebuild EVERYTHING.** `up`'s "is there
   a store?" check counts nodes in the ROOT store. Root empty (because of
   1) ⇒ `no local store — gathering from source` ⇒ all modules re-gathered
   from scratch, twice in the transcript, while 18 perfectly good module
   stores sat on disk.
4. **Config edits between runs were invisible** (the gap the superseded
   ADR chased with snapshot hashes): edit `modules[]` without committing ⇒
   git HEAD unchanged ⇒ `up` no-ops.

Root cause common to all four: the checks (shrink guard, store-exists,
freshness) each look at ONE store's local state, while the thing that
defines what SHOULD exist — `.ctxoptimize/config.json`, already loaded at
the start of every verb — is never consulted.

## Decision — reconcile against the PRESENT config, keep no history

`config.json` declares the desired world (module list, names, sources).
Every run reads it anyway. So instead of remembering what the config used
to be (hashes, stamps — the superseded ADR), every `up`/`add` RECONCILES
observed store state against the config in hand. Change-over-time tracking
becomes unnecessary: add a module → its store is missing → gather it;
remove one → its store is undeclared → say so. Three changes, no new
persisted state:

### 1. Shrink guard becomes config-scoped

- `modules[]` present ⇒ the root store is a RESIDUAL by definition ⇒ the
  count-shrink refusal is SKIPPED for the root store only. Rationale: the
  residual's size follows the module list; a raw count comparison there is
  meaningless by construction (206717→3018 was CORRECT).
- Every module store keeps the guard exactly as today — module scope is
  stable, so a >50% drop there still means a broken gather until a human
  says otherwise.
- Single-module repos (no `modules[]`): unchanged — the guard's original
  design case (a partial checkout must not wipe a good store) still holds.

### 2. `up` reconciles the DECLARED module set, not the root node count

`up`'s existence/refresh decision walks `modules[]` from the config:

- For each declared module (plus the root residual): store missing →
  gather it; store present and behind git HEAD → fast re-gather; store
  present and current → leave it alone.
- The root store's node count no longer gates anything. A broken residual
  re-gathers the residual — never the other 18 modules.
- This is also what closes the superseded ADR's gap WITHOUT hashes: a
  module added to config (uncommitted or not) is simply a missing store on
  the next `up`; a renamed store name is a missing store under the new
  key. The present config is the whole signal.

### 3. A failed gather leaves the module un-provenanced — and `up` says so

- Provenance (`source.json`) is already written only on gather SUCCESS.
  Reconciliation treats "declared module with no/old provenance after the
  code moved" as needing refresh — so a module whose gather failed cannot
  ride a green `up: store ready` on the next run; it shows up in the
  reconcile as pending and is retried (or its failure re-surfaced).
- The `up` summary names per-module outcomes when anything was not clean:
  `up: 18 modules current · 1 re-gathered · 1 FAILED (.) — see above`.

### Orphaned stores (module removed from config)

LOG, never delete (matches the house shape: shrink guard, tracked-.env —
detect, say it plainly, name the one command, never act alone):
`store exists but is no longer declared: apps/librechat/packages/mcp —
remove with: ctx-optimize store delete <key>` printed by `up`; the store
otherwise ignored (not federated, not refreshed).

## What this deliberately does NOT do

- **No config snapshot/hash/stamp** — superseded ADR's mechanism; nothing
  persisted about past configs. Reconciliation reads only the present.
- **No navigator-only root** — the residual stays; top-level loose files
  keep being indexed. (Rejected: solves 1 and 3 by amputation, loses
  coverage, forces migration.)
- **No auto-delete of orphans, no auto `--force`** — every destructive
  resolution stays a human command.
- **Freshness package untouched** — git-HEAD comparison remains the code
  staleness signal; reconciliation wraps AROUND it per module rather than
  extending it.

## Gates

- Hermetic CLI tests reproducing all four transcript failures:
  (a) module-list edit → root residual re-gather does NOT trip the guard;
  (b) module store keeps the guard (real shrink still refused);
  (c) failed module gather → next `up` does NOT report clean, retries or
  re-surfaces that module only;
  (d) empty/broken root + populated module stores → `up` refreshes the
  residual only, never the full fan-out;
  (e) module added to config (no commit) → next `up` gathers exactly it;
  module removed → orphan logged, store left on disk.
- `task ci` + `task golden` green; single-module behavior byte-identical
  (golden already pins it).
- Dogfood on volentis: from the current fossil state, one `up` must
  converge with zero `--force` and zero full rebuilds.
