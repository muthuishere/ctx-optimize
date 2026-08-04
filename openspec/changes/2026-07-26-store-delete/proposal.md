# ADR — `store delete`: remove ONE store, and mean it

Status: **IMPLEMENTED** — 2026-07-26.

## Context

There was no CLI way to delete a store. `uninstall` explicitly leaves them
(`"stores + committed repo pointers untouched"`), and the only delete path was
the dashboard's `DELETE /api/store`. So the practical answer was
`rm -rf ~/ctxoptimize/<name>` — aimed by hand at a root that holds **every**
repo's store plus `audit.ndjson`, with no confirmation and no audit trail.

`CLAUDE.md` states that dashboard mutations are "routed through the SAME cmd
funcs the CLI dispatches (Ops closures from internal/app)". That was already
false for exactly this operation: `handleStoreDelete` deleted directly and had
no CLI counterpart to route through.

## The bug found while building it

A store dir is **not a leaf**. A multi-module repo nests its module stores
inside the root store:

```
~/ctxoptimize/reqsume/                  ← root store
~/ctxoptimize/reqsume/e2e/              ← a DIFFERENT store
~/ctxoptimize/reqsume/regressiontest/   ← another one
```

The dashboard did `os.RemoveAll(dir)`, so **deleting the root store silently
destroyed three stores while reporting one.** Verified against the real store
root on this machine, where `reqsume` has exactly that shape.

A second hole surfaced from a test rather than from reading: `SanitizeKeyPath`
drops a `..` segment (`sanitizeKey("..")` trims to `""`), so the key `repo/..`
was rewritten to `repo` — no traversal escape, but **a delete of a store the
caller never named.** Sanitizing is right for creating a key and wrong for
destroying one.

## Decision

`store.Delete(root, key, withNested)` in `internal/store`, and
`ctx-optimize store delete` on top of it. Both the CLI and the dashboard now go
through it.

Guards, each closing something that was actually reachable:

| guard | why |
|---|---|
| key must survive `SanitizeKeyPath` **unchanged** | `repo/..` deleted `repo` |
| target must not be the store root | it holds every store + the audit log |
| target must not escape the root | prefix-checked after `Abs` |
| target must have a `graph/` dir | a typo cannot delete an unrelated dir under the root |
| nested stores are **kept** by default | `RemoveAll` destroyed 3 stores, reported 1 |

CLI shape:

- **Key comes from cwd**, resolved exactly as `add`/`status` resolve it (config
  `name`, else the module key). There is no positional argument, so the verb
  cannot be pointed at an arbitrary directory. `--path DIR` selects a different
  module, still by key.
- **Dry-run by default.** It prints what would go, names the nested stores that
  would survive, and says `.ctxoptimize/` is untouched. `--yes` performs it. A
  confirmation that does not state the blast radius is theatre.
- **The target is always the whole REPO**, resolved from the scope's root key
  whichever directory you run it from: the root store plus every module store,
  at any depth. There is no per-module delete — module stores are the same
  repo's derived data and re-gathering takes seconds, so the option was surface
  with no use case (owner-directed).

  The first version kept nested stores by default and chromium exposed why that
  is wrong: it printed `deleted store "chromium"` and left **33 chromium module
  stores** on disk. Reporting one deletion while 33 survive is a lie by
  omission. What stays impossible is reaching a store the caller never named.

  Two bugs fell out of testing this rather than reading it. The per-module path
  resolved its key with `ModuleKey`, yielding `svcB` where the store lives at
  `dtest/svcB` — so a delete from inside a module always missed; it is gone with
  the module path. And `nestedStores` stopped at the first store it found
  (`SkipDir`), so a repo declaring both `svcB` and `svcB/inner` reported 2
  stores where there were 3. The delete was correct either way, but a prompt
  that UNDER-states its blast radius is the one error direction that is never
  acceptable.
- **It ASKS.** `[y/N]` at a terminal, after printing the blast radius —
  requiring a second full invocation of the command added no safety, only
  typing. Off a terminal nothing is asked and nothing is deleted: a missing
  answer must never read as consent, so `--yes` is the only non-interactive
  path. `/dev/null` needed excluding explicitly — it is a character device, so
  the first TTY check called it a terminal, printed a prompt, read EOF and
  reported "cancelled": the safe outcome by luck with the wrong explanation.
- **Audited** (`store.delete` in `audit.ndjson`), like every dashboard mutation.
- `.ctxoptimize/` is never touched: it is committed config, not a cache.
  Deleting tracked files is a different act from dropping derived state.

## Verified

```
$ ctx-optimize store delete
would delete store "deltest" at /tmp/deltest-root/deltest
  .ctxoptimize/ in the repo is NOT touched; re-gather with `add .`
pass --yes to do it

$ ctx-optimize store delete --yes
deleted store "deltest" → /tmp/deltest-root/deltest

$ ctx-optimize store delete --yes
ctx-optimize: store delete: … is not a store (no graph/ dir) — nothing removed   [exit 1]
```

`TestDeleteKeepsNestedModuleStores` (asserts siblings AND nested stores survive
byte-for-byte), `TestDeleteWithNestedRemovesEverything`,
`TestDeleteRefusesTheStoreRoot` (six spellings of "the root", including
`repo/..`), `TestDeleteRefusesNonStoreDirs`, `TestPreviewDeleteTouchesNothing`.
`task ci` green; the dashboard keeps its 404-on-unknown-store contract.

## Not built: delete-and-rebuild

The original ask included "resync — delete and rebuild". Not shipped, because
whether that is a CONVENIENCE or a CORRECTNESS fix is still unmeasured: `Replace`
prunes producer-scoped and `sources.Reconcile` prunes retired source producers,
but whether a retired ADAPTER leaves nodes in the store forever has not been
checked. If it does, rebuild fixes a real staleness bug and deserves to be more
than sugar for `store delete --yes && add .`. That check comes first.

## Not claimed

- The nested-store repair is verified on synthetic fixtures plus a read of the
  real store root; no destructive test was run against real data.
- No claim that anyone wants `--keep-nested`. It exists because `RemoveAll`
  cannot express "this store but not the ones inside it" at all, and because a
  guard is worth having even when the default goes the other way.
- TTY detection is stdlib-only (`ModeCharDevice` minus `os.SameFile` against
  `os.DevNull`). It has been verified against a real pty, a pipe and
  `< /dev/null`; exotic terminals are unverified, and the failure direction is
  "refuse and ask for --yes", never "delete".
