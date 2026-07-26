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
- `--with-nested` opts into the recursive delete — always an explicit request.
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
- No claim that anyone wants `--with-nested`. It exists because the alternative
  was leaving `RemoveAll` reachable, and because "drop this whole monorepo"
  is a legitimate request that should be spelled out rather than implied.
