# ADR — "the progress bar takes too long" was a quadratic

Status: **IMPLEMENTED** — 2026-07-26.

## Context

Reported as a progress-bar problem: a big `add` prints `[47/48]` and then goes
silent for a long time. The progress display was the symptom. The cause was an
O(n² log n) loop, and finding it took three wrong guesses that are worth
recording, because each one *looked* right.

## The measurement chain

Synthetic repo: 12,000 root `.go` files (the residual) + 20 tiny modules.
Baseline `add`: **16.5s**, of which ~16s is after the last module tick.

| guess | test | result |
|---|---|---|
| the residual runs alone, wasting cores | sample process CPU% | **wrong** — 880% burst (8 cores) then ~100% for 18 samples. Something IS serial, but extraction is not it |
| the serial tail is wiki I/O (12k atomic writes) | `--no-wiki` | wiki = **13.6s of 16.6s (82%)**, so the wiki is the phase — but a standalone benchmark of 12,000 tmp+rename writes is only **1.23s**, so writing is not the cost |
| the cost is rendering 12k pages | parallelize the page loop across 8 cores | **no change** (16.31 → 16.88s). Byte-identical output, so the change was correct — just not the bottleneck |

Only then, timing each phase of `wiki.Generate` directly:

```
Nodes()         11ms  (24000)
Edges()          5ms  (12000)
newGraph()       8ms
Hubs()           4ms
Communities() 12.809s  (0 communities)
Generate() ALL 14.238s
```

**`analyze.Communities` was 90% of the wiki's time and returned nothing.** A CPU
profile put 63.5% cumulative in `sort.partition_func`.

## The defect

The dust-merge phase (`communities.go`) needs one thing per iteration: the
smallest non-isolated community. It got it by **rebuilding the entire community
list and re-sorting it, every iteration**:

```go
for len(members) > 1 {
    list := make([]cs, 0, len(members))   // all communities
    for id, ms := range members { … }
    sort.Slice(list, …)                    // sort ALL of them
    // …then look at exactly ONE: list[0]
```

12,000 dust components → ~12,000 iterations × a 12,000-entry sort ≈ 1.7B
comparisons. And every one of those components is disconnected dust that gets
dropped, so the answer was 0 communities: **12.8 seconds to produce nothing.**

This shape is not contrived. It is what a large repo of mostly-unconnected files
looks like — 12,000 files each declaring one never-called function. Chromium's
366k-node residual is the same shape at 30× the size.

## Fix

A min-heap over `(size, id)` with lazy invalidation. Same candidate, same
tie-breaking (size ascending, ties by ascending id — which is what keeps
clustering deterministic), O(log n) per step instead of O(n log n).

Entries are immutable snapshots; a community whose size changed gets a new entry
pushed and its stale one discarded on pop. Two subtleties that would have been
bugs: an `isolated` community is popped and never re-pushed (isolation is
permanent, matching the old `continue`), and a candidate that turns out to be
big enough must be pushed BACK before breaking out — leaving the merge loop is
not the same as consuming it.

## Also fixed: parallel wiki page writes

Worth **15%** of a large gather (4.76s → 4.03s). Notable because it measured as
**worthless** the first time — the 12.8s quadratic was drowning the signal. It
was reverted on that measurement and reinstated once the real bottleneck was
gone. A perf change measured against a background of noise is not measured.

## Results

| | before | after |
|---|---:|---:|
| `add` on the 12k-file repo | **16.46s** | **3.89s** (4.2×) |
| `Communities` on 12k dust components | ~12.8s | **16ms** |

Clustering output is unchanged: `report --json` on this repo gives byte-identical
subsystems (10, same members, same order). Wiki output verified page-by-page —
0 of 12,021 pages differ.

## Why the existing perf test missed it

`TestCommunities50kUnderASecond` passes in 29ms on 50,000 nodes — its graph is
connected, so the dust-merge loop barely runs. The ceiling was real and the shape
was wrong. `TestCommunitiesDustMergeIsNotQuadratic` adds the missing shape, with
a deliberately loose 3s ceiling: it guards against a return to quadratic (13s vs
16ms), not against constant factors on a slow CI box.

## Not claimed

- The progress display was NOT changed. It is still completion-only, so a single
  long task still prints nothing while it runs — 4× shorter now, but chromium's
  residual is still minutes of silence. A start-of-task indicator or a heartbeat
  is a separate, unbuilt improvement.
- The 4.2× is one synthetic shape chosen to stress the dust path. Real repos with
  well-connected code spend proportionally less time here; the corpus tier
  (linux 0.4s, Newtonsoft 1.0s) is unchanged, which is the honest counterweight —
  this fix does nothing for graphs that were never in the bad shape.
- No profiling of what now dominates the 3.89s. Extraction and the wiki are the
  remaining candidates and neither has been attacked.
