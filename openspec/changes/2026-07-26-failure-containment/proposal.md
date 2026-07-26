# ADR — one break must not stop the whole

Status: **IMPLEMENTED** — 2026-07-26.

## The question

"Why does one break stop the whole?" Asked about the chromium run. The answer was
that it didn't at one level, did at two, and at a fourth level something never
stopped at all.

| level | before | after |
|---|---|---|
| one bad **file** | skipped, gather continues (`code.go:426`) | unchanged — already right |
| one bad **producer lane** | **7 early returns.** Worst shape: the adapter lane returned AFTER code/docs/manifests were extracted but BEFORE the commit loop, so one broken adapter script **discarded a whole successful gather** | every lane runs, everything that worked is committed, failures reported together |
| one `Replace` at commit time | aborted committing the REMAINING producers | each producer commits or fails alone |
| one bad **module** of 48 | all modules ran, but `if len(failed) > 0 { return }` fired **before** `writeNavigator` — 47 good modules, no navigator, no root federation | navigator written first, failures reported after |
| a **retired** producer | never pruned. Ever. | reported every gather; pruned on a complete `--force`; `--rebuild` guarantees it |

## Measured: the retired-producer bug

`Replace` is producer-SCOPED, so a producer that stops running is never replaced
and its nodes have nothing to prune them. Reproduced directly: an adapter
emitting `custom://ghost`, then deleted, then

```
$ ctx-optimize add . --force
orphaned adapter nodes still in store: ['custom://ghost']
```

It survived `--force` and every subsequent gather. Same failure mode
`sources.Reconcile` already handled for *source* producers; nothing covered the
rest (adapters, a removed grammar pack's language, a renamed producer).

## Decisions

### Lanes are contained, and the incompleteness is RECORDED

Containment alone would be a downgrade: a store quietly missing its code lane
answering as though it were complete is worse than a failed gather. So
`freshness.Source` gained `Partial []string` — which lanes failed — and:

- a partial gather **clears the tree signature**, so the next run cannot
  short-circuit as "unchanged" and freeze the gap in place;
- `gatherInto` still returns a non-zero error naming every failed lane, after
  committing what worked.

### Retired producers are REPORTED, not silently pruned

Absence from a gather means either "retired" or "did not run this time"
(`--no-adapters`, unchanged git HEAD, a failed lane), and those are
indistinguishable from inside the store. Silently deleting a lane's data because
it did not run would be far worse than a stale node. So pruning requires a run
with **no skips and no failures**, plus `--force`. Otherwise it prints:

```
note: 1 retired producer(s) still in the graph: mine — they no longer run, so their nodes are stale.
      prune with `ctx-optimize add . --force` (a complete run), or `store delete --yes && add .` to rebuild.
```

### `add --rebuild` is the certain path

Drops the store(s) this add will write, then gathers into nothing. Uses the SAME
task plan as the gather, so it cannot drop a key the gather won't rewrite.
Nested module stores are KEPT (`store.Delete`'s default) — rebuilding a monorepo
root must not destroy its modules' stores; each is rebuilt by its own task.
Audited as `store.rebuild`.

## Why the navigator fix is a sequencing fix, not a policy change

`writeNavigator` builds from the **full task plan**, not from the successes — its
own doc comment says so, because `up`'s partial reconcile must never shrink the
navigator to the subset it gathered. So writing it after a partial failure was
always safe; the early `return` was simply in the wrong place.

## Verified

- `TestBrokenAdapterDoesNotDiscardTheGather` — the code lane's `Real` is in the
  store even though the adapter exited 3, and the run still exits non-zero.
- `TestPartialGatherIsRecordedAsPartial` — `partial` is populated and `tree_sig`
  is cleared.
- `TestRetiredProducerIsReportedThenPrunable` — reported on a plain add,
  gone after `--rebuild`, real content still present.
- `task ci` green; hermetic + corpus golden green; judged tiers unmoved
  (16.5 / 13.0).

## Not claimed

- Lane containment is tested through the adapter lane, which is the one with a
  user-supplied failure mode. The code/docs/manifest lanes fail only on
  conditions I could not trigger hermetically, so their containment is verified
  by construction (the shared `lane` helper), not by a test per lane.
- `Partial` is recorded but `status` / `fresh` do not yet SURFACE it. The record
  is what makes surfacing possible; wiring it into the freshness verdict is a
  follow-up, and until then a partial store is detectable only by reading
  `source.json` or the gather's non-zero exit.
- No claim about how often any of this fires. The chromium run is one data point
  and it did not itself hit a lane failure — it hit the noise and discovery
  problems fixed separately.
