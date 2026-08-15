# Roadmap — beating the field, from measured position

Written 2026-08-15 from the session's measurements. Every line here has a
number behind it; where a number is stale or missing, it says so.

## Where we actually stand

**Won, decisively:**

| axis | us | best competitor |
|---|---|---|
| cold build, kubernetes | **7.32s** | graphify 249.65s (**34×**) |
| cold build, linux | **55.14s** | — (none completes) |
| re-gather, no change | **0.25s** | graphify 267.54s (**1,070×**) |
| context efficiency (correctness/byte) | **0.000132** | codegraph 0.0000478 (**2.8×**) |
| external-API / boundary ports | **only tool that models it** | none |
| determinism (byte-identical re-gather) | **yes** | unmeasured elsewhere |

**Lost, with the cause identified:**

| axis | us | best | cause |
|---|---|---|---|
| answer correctness | 0.79 | codegraph **0.86** | see P1 — partly stale |
| answer coverage | 0.79 | codegraph **1.00** | we omit sig+doc in `query` |
| query latency, linux | 3,516ms | codegraph **536ms** | reads all 855MB before knowing the question |
| peak RSS, reqsume | 9.40GB at full throttle (was 12.4GB; **0.73GB** at GOMAXPROCS=2 since `e54dd6f`) | graphify **429MB** | modules × workers × 64MB |
| one-file re-gather | 91% of cold | unmeasured | store write is O(whole graph) |

Competitor rows are 2026-07-24 and **unpinned** (the arena has no
`versions.json`). Treat as indicative until `setup.py` rebuilds it.

## P0 — memory (D0 SHIPPED 2026-08-15, remainder open)

**Done:** worker pools now key on `runtime.GOMAXPROCS(0)`, not `NumCPU`
(commit `e54dd6f`). `NumCPU` ignored cgroup quotas, so a 2-CPU container on a
64-core host spawned 63 wasm instances. Measured on reqsume's 7 modules after:
**0.73 GB at GOMAXPROCS=2**, 2.66 GB at 6, 9.40 GB at 18 — all were ~12.4 GB
before, and GOMAXPROCS is now a working memory cap. Full throttle on a laptop
is unchanged.

**Open:** at full throttle the multi-module case still costs 9.40 GB, because
the budget MULTIPLIES — module fan-out x per-module workers x 64 MB. ADR 12
D1 (global instance budget) / D2 (the 64 MB floor).

### The original framing, kept for the numbers

reqsume (7 modules, 4,883 files) peaks at **10.58 GB**. An 8 GB CI runner
cannot gather it. graphify does the same tree in 429 MB. Cause is measured:
workers are `NumCPU-1` (=17), each wazero instance starts at 64 MB, modules
gather in parallel → `modules × 17 × 64MB` = 7.6 GB of guest memory alive.

**Target: under 2 GB on reqsume, byte-identical output.** ADR 12 D1/D2.
This is first because every other win is unreachable by a user who OOMs.

## P1 — reclaim the quality rows (cheapest real win)

Two moves, both small:

1. **Re-run the judged quality bench at HEAD.** The July run scored
   `ctx-optimize card` at 0.66 because it answered `url_for [module]
   module://url_for` with no definition. That is the unresolved `module://`
   placeholder D7 fixed. Verified today: the same question now returns
   `url_for [function] src/flask/helpers.py L200-L251` with signature and
   body — 0.5 → 1.0 correctness, 0.5 → 1.0 coverage on that question alone.
   **The published 0.79/0.66 understate HEAD. Re-measure before optimising
   anything.**
2. **Close coverage by emitting what codegraph emits.** It scores 1.00 by
   always showing signature + docstring; our `query` shows location and a
   snippet. `card` already produces the richer form. Make `query`'s top hits
   carry sig+doc. Watch the efficiency metric while doing it — our 2.8× win is
   correctness *per byte*, and paying 18KB like codegraph to match its
   coverage would trade our strongest axis for its strongest.

**Target: correctness ≥ 0.86, coverage ≥ 0.90, context under 8KB.**

## P2 — the two speed axes we lose

3. **Query index** (ADR 12 D3). `card` went 1.8s → 22ms with a lookup index;
   `query` still deserializes 855MB before it knows the question.
   **Target: under 500ms on linux**, matching codegraph, with judged scores
   not moving down.
4. **Incremental store write** (ADR 11 D1). A one-file edit costs 91% of a
   cold build because `store.ReplaceAll` rewrites all 334k nodes every time.
   **Target: one-file re-gather under 20% of cold.** This is the work that
   makes the multi-pass positioning true — until it lands, our session
   argument rests on the no-change case, which is not what anyone does.

Do NOT build the per-file extract cache first: it is spiked and works (5–10× on
the extract phase) but the gather barely moves because the store write
dominates, and it costs 250MB of disk and a 15–25% slower cold gather.

## P3 — strategic, once the above holds

5. **Lead with boundaries.** No measured competitor models "what external APIs
   does this repo call". It is the one capability we have and nobody else does,
   and it is now free (rides the AST walk, ~1% of gather).
6. **Get an external yardstick.** The judged 20-question scoreboard is
   self-authored, self-graded and self-floored — useful as a regression net,
   worth ~nothing to a skeptic. Loc-Bench scores at file/module/function
   granularity, which maps 1:1 onto our node kinds.
7. **Rebuild the arena with `setup.py`** so competitor numbers are pin-verified
   before any of this is published, and fix README's "2.5× faster than the next
   tool" to name its cohort.

## The optimisation ceiling — what is left after all of it

From the pprof pass: **58% of CPU is tree-sitter parsing inside wasm** and ~15%
is zeroing driven by output volume. Neither is reachable from Go — the mutex
and block profiles show no contention, so atomics and pools have nothing to
bite on. Two levers remain, both already measured:

- **Parse less** (ADR 8): one 1.8MB generated `.pb.go` yields 8.7M AST records.
  Skipping generated files changes the graph, so it is a product decision, not
  a perf tweak — and generated code genuinely calls hand-written code.
- **Emit less** (ADR 9): 37% of the wasm→Go record stream is anonymous nodes
  every consumer discards. Spiked and verified safe, but only −1.0% on
  kubernetes, so it ships only if a wasm rebuild happens anyway.

Everything else is bookkeeping we already removed.
