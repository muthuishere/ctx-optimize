# Spikes — wiki at scale (2026-07-24)

**The spike overturns the ADR's premise.** Measured `wiki.Generate` at true
drivers scale (1,680,000 nodes / 1,679,999 edges / 30,021 file pages), on real
APFS SSD (macOS `t.TempDir`, not tmpfs):

| sub-step | time | O(?) |
|---|---|---|
| store.Nodes() load (parse ndjson) | **161 ms** | O(nodes) |
| store.Edges() load | **156 ms** | O(edges) |
| newGraph (index + sort neighbors) | ~1.0 s | O(nodes+edges) — global |
| Hubs | ~0.8 s | global |
| Communities | ~1.3 s | global |
| slug names | 12 ms | O(files) |
| renderFile (CPU, all pages) | ~0.7 s | O(files) — per-file |
| writeAtomic (disk, all pages) | ~3.3 s | O(files) — per-file |
| index render | 46 ms | global |
| **FULL Generate() end-to-end** | **~8.9 s** | |

## CORRECTION (real Linux run, 2026-07-24) — premise CONFIRMED, my synthetic was too sparse

M0 done: the full Linux `drivers`+tree gather (scale-robust partition fix) gives
ground truth, and it **overturns my synthetic's verdict**:

| | real Linux | my synthetic |
|---|---|---|
| nodes / edges | **2.85M / 4.6M** | 1.68M / 1.68M |
| wiki pages | **60,388** | 30,021 |
| extraction | **18.9 s** | n/a |
| **wiki regen** | **~1,462 s (≈98% of a 1,481 s add)** | ~9 s |
| store size | **2.0 GB** (≈all wiki) | small |
| **per page** | **~24 ms, ~33 KB** | ~0.3 ms, tiny |

The **~80× per-page gap is node DEGREE.** `renderFile` is O(degree) — it renders
every neighbor of the node into the page. Linux kernel hub nodes have thousands
of neighbors, so real pages are ~33 KB (→ 2 GB total) and cost ~24 ms each to
render+write. My synthetic gave each file ~56 sparse edges → ~sub-KB pages, which
under-measured the per-page cost ~80× and produced the wrong "wiki is only 9 s"
verdict. **The owner's attribution is correct: at Linux scale wiki regen is ~98%
of the wall time AND ~all of the 2 GB — extraction is 18.9 s of 24.7 min.**

⇒ The plan below stands, but its urgency and mechanism sharpen: the cost is
**per-file render+write scaling with degree**, so (a) M1 (autosync skips wiki) is
mandatory — a background resync must never pay 24 min — and (b) M2 (incremental:
re-render only changed pages) is the real interactive fix, since the per-file
work is exactly what incrementalizes. The global floor (newGraph/Hubs/Communities
at 2.85M/4.6M) also needs its own measurement — it may no longer be a ~few-second
floor at this scale. A hard page-size cap (truncate a 33 KB neighbor dump to top-N)
is a third cheap lever worth spiking.

## Verdict (synthetic — SUPERSEDED by the real run above) — wiki is NOT the 12-minute cost

The reported Linux `drivers/` run was **735 s (12 min) / 1.1 GB**, attributed to
wiki because it "generated 30,760 pages." **Reproduced at that exact scale,
`wiki.Generate` costs ~9 s, not 735 s.** Store load is ~0.3 s. The 12 minutes is
**something else** — extraction, git-history, real-corpus I/O, or monorepo
fan-out — NOT wiki regen. `writeAtomic` is write+rename, no fsync; 30k pages =
~3.3 s even on SSD. **The premise "wiki regen dominates at scale" is false as
measured.** Re-attribute before optimizing (measure the real run's sub-steps —
`add` already prints `code:` / `git-history:` / `wiki:` lines).

## What IS true and still worth acting on

1. **Wiki is the single largest component of a resync** (~9 s of it), and it is
   **O(all files)** — a 1-file edit re-renders all 30k pages. For **autosync**
   (frequent background resyncs) that is pure waste on the hot path.
2. The per-file work (renderFile 0.7 s + writeAtomic 3.3 s = **~4 s, 45% of the
   9 s**) is trivially incrementalizable — it is per-file and deterministic.
3. The **global** floor (load + newGraph + Hubs + Communities + index ≈ **~4.6 s**)
   is NOT per-file; an incremental render still pays it unless the hub/community
   structure is hash-guarded and skipped when unchanged.

## The better option (spike-driven)

Drop the threshold/opt-out band-aid (it was sized against a wrong 735 s premise).
Two clean, deterministic moves instead:

- **M1 (cheap, high value): autosync never regenerates the wiki.** The lazy/block
  code resync refreshes the graph only; the wiki refreshes on an explicit
  `add`/`up`/`sync`. ~10 lines (thread a `skipWiki` into `gatherInto`; auto-sync
  child passes it). Removes the entire wiki cost from the autosync hot path — the
  original worry — with zero risk to interactive output.
- **M2 (durable): incremental file-page render.** Re-render only file pages whose
  source node content-hash changed (delete orphaned pages); recompute
  hubs/communities/index only when the hub/community set hash changes. Collapses
  an interactive 1-file `sync` wiki from ~9 s toward the ~1.3 s global floor
  (load + newGraph). Deterministic; golden-gated `incremental == full`.

- **M0 (do first): re-measure the real 735 s** to find the actual dominator. If
  it is extraction/IO, M1+M2 help autosync but the "12 min" headline needs a
  different fix. Do not build against an unverified attribution.

## Recommendation

M1 now (it is the real fix for the stated autosync concern and nearly free), M0
in parallel to find the true 735 s cause, M2 only if interactive `sync` wiki time
proves to matter after M0. A `wiki: off|on` config knob is still worth adding for
control, but it is not the performance mechanism.
