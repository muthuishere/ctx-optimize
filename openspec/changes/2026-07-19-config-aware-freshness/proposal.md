# Config-aware freshness — `up` must rebuild when `.ctxoptimize/config.json` changes

Date: 2026-07-19 · Status: SUPERSEDED by 2026-07-19-config-reconciliation — snapshot-hash freshness dissolved once every `up` reconciles against the PRESENT config; implementation was reverted unmerged

## Problem — observed, not hypothetical

Owner report (2026-07-19): "if config.json file changed calling up is not
rebuilding."

Root cause, verified in `internal/app/app.go` (`upCore` →
`freshnessReports`): freshness is a **pure git-HEAD comparison**. The store
records `{path, head, head_unix, added_unix}` per gathered root
(`source.json`, written in `internal/app/multimodule.go`'s single
RecordSource site); `up` compares recorded head vs current head and
declares Fresh on equality.

`.ctxoptimize/config.json` is the gather's *recipe* — module list, store
name, sources[], remote. Editing it **without committing** leaves HEAD
unchanged, so `up` prints "up to date with git HEAD" and no-ops. The user
who just added a module to the config gets a store that silently ignores
it. (A *committed* config edit moves HEAD and already triggers the stale
lane — the gap is exactly the uncommitted-edit window, which is also the
normal authoring flow: edit config → run `up` → commit once it works.)

Related real-world case, same week (volentis): a root store predating a
module split refused a legitimate 206k→11k shrink. Config-aware freshness
would have re-gathered at declaration time instead of leaving a fossil.

## Decision

Stamp a **content hash of the effective config** into each recorded source
and treat a hash mismatch as **Stale**, even when git HEAD is unchanged.

1. **`freshness.Source` gains `config_sha`** (sha256 hex of the
   `.ctxoptimize/config.json` bytes governing that gather; `""` when no
   config exists or the record predates this change). `source.json` stays
   git-diffable; the field is `omitempty` so legacy files parse unchanged.
2. **`freshness.Report` gains `config_changed`** so `status` / `fresh
   --json` can say *why* it is stale. Nothing else is added — no
   timestamps, no mtimes: one hash at record time, one compare at check
   time.
3. **`Evaluate` compares the hashes first**: both non-empty and different ⇒
   `Stale` + `ConfigChanged`, before any head comparison (a config edit at
   the same HEAD must not be masked by "heads equal"). Either side empty ⇒
   config check skipped (legacy records and configless repos keep today's
   behavior exactly — no retroactive stale storms on existing stores).
   Deleted config (recorded non-empty, current "") is deliberately NOT
   flagged: walk-up failure and deletion are indistinguishable here, and a
   false Stale is worse than a missed rare case.
4. **The hash is of the config that governs the gathered root**, found by
   the same upward walk the CLI already uses for config discovery — module
   stores record the ROOT repo's config hash, since the module list lives
   there. Helper: `project.ConfigSHA(startDir) string` (walk up to the
   first `.ctxoptimize/config.json`; sha256 of raw bytes; "" if none).
   Raw-byte hashing means formatting-only edits re-gather too — accepted:
   re-gather is cheap and the rule stays explainable ("the file changed").
5. **Stale message names the cause**: `✗ STALE — .ctxoptimize/config.json
   changed since gather; run: ctx-optimize add .` (the head-based message
   is unchanged). `up`'s stale lane already runs the fast re-gather; it
   needs no new logic — only the verdict had the blind spot.

## Rejected alternatives

- **Compare config mtime** — rsync/checkout churn gives false stales;
  content hash is exact and already the house pattern (store manifest).
- **Full dirty-tree detection (`git status`)** — any WIP edit anywhere
  would flag the store stale; the recipe file is special, source files are
  not (their staleness is what HEAD comparison is *for*).
- **Hash the parsed/normalized config** — silently forgives formatting
  edits but adds a normalization contract that must never drift between
  versions; raw bytes are dumber and safer.
- **Make `up` always re-gather** — destroys the no-op promise that makes
  "run `up` whenever" safe to say.

## Gates

- Unit: `Evaluate` table gains config-hash cases (mismatch ⇒ Stale +
  ConfigChanged at equal heads; legacy ""-hash records unchanged verdicts).
- CLI: hermetic test — `init` + gather, edit config.json only (no commit),
  `fresh` exits 1 and `up` re-gathers; untouched config stays a no-op.
- `task ci` + `task golden` green; no query-path cost (hashing happens
  only in `add`'s record step and `fresh`/`up`'s check step).
