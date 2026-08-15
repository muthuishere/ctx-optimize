# ADR 11 — incremental gather: the store write is the bottleneck, not the parse

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: `internal/store` (incremental write) and, second, a per-file extract
cache in `internal/extract/code`. No schema change, no emitted-fact change.
Written from a working spike, not a design sketch — the numbers below are
measured on a prototype that runs.

## The business case, measured 2026-08-15 — our positioning is not yet earned

The multi-pass session benchmark (`benchmarks/session/`) exists to argue that a
graph tool wins over an agent SESSION even when it loses the cold build,
because incremental re-index amortizes it away. Measured on go-kubernetes at
HEAD, that argument does not currently hold for us:

| scenario | wall | vs cold |
|---|---|---|
| cold build | 7.79s | — |
| **one file edited, re-gather** | **7.10s** | **saves 9%** |
| nothing changed | 0.25s | saves 97% |

The zero-change short-circuit is excellent and is what earlier measurements
(and an earlier audit's "incremental is unaffected") were actually reporting.
But the case an agent lives in — *I changed one file, tell me what that
means* — costs **91% of a full rebuild**. There is nothing to amortize, so the
multi-pass thesis is currently a claim our own benchmark does not support.

⇒ **This ADR is not a performance nicety. It is the work that makes the
product's positioning true.** Ship it before publishing any multi-pass
comparison.

(Measurement note: a first attempt showed a suspicious 0.25s for the one-file
case. The edit had landed in a directory `treeSignature` skips — `build`,
`dist`, `node_modules`, `vendor`, `target`, `*-out`, dotdirs. Re-run against a
file *proven* to be a node in the store, it is 7.10s. Any future measurement
must pick its victim file from the graph, not from `find`.)

## The goal

A one-file edit should cost roughly one file of work. Today lever 1
(`internal/app/multimodule.go:735`) is a whole-tree stat signature consumed by
an early return: signature matches, skip everything; one byte differs,
re-extract the entire module.

## The spike built the obvious fix — and it exposed the real bottleneck

A per-file cache keyed on content hash was prototyped and works. Phase split on
go-kubernetes (`--force`, per gather):

| phase | cache off | cache on |
|---|---|---|
| per-file extract + collect | 1.47–3.18s | **0.27–0.36s** |
| global (call resolution, routes, importresolve) | 0.35s | 0.36s |
| code lane total | 2.58–2.85s | 1.45–1.52s |
| **`store.ReplaceAll`** | **3.86s** | **4.30s** |

**The cache delivers 5–10× on the phase it targets and the gather barely
moves**, because `ReplaceAll` reads, merges, sorts and rewrites all **334,002
nodes on every gather regardless of how little changed**. Amdahl does the rest.

Wall-clock, one-file-touched:

| corpus | cache off | cache on |
|---|---|---|
| ts-typescript (36,773 files) | 12.24s | **5.34s** (−56%) |
| go-kubernetes (10,583) | 9.46s | 7.59s (−20%) |
| java-spring (8,350) | 3.41s | 3.48s (**no gain**) |

The win tracks the **extract:store ratio**, not corpus size — java-spring gains
nothing because its store write already swamps a small extract phase. And cold
gathers get **15–25% slower** (kubernetes 7.48 → 8.96s): you pay serialization
to fill a cache you have not used yet.

⇒ **Shipping the cache alone would add a 250 MB on-disk cache and a slower
first gather to buy 0–20% on two of three corpora.** That is not a good trade.

## D1 — make the store write incremental (do this FIRST)

`ReplaceAll` already collapsed N producer passes into one (commit `7d748dc`,
which halved gather time). The remaining cost is that one pass is still O(whole
graph): every node re-read, re-sorted and re-written even when a single file's
facts changed.

Target: write only what changed. The store is sorted ndjson plus a content-hash
manifest, so the shape of the answer is a merge that rewrites affected regions
rather than the whole file, or a per-producer segmented layout. Constraints
that cannot bend: output stays **sorted, deterministic and git-diffable**, and
atomic rename semantics are preserved — the concurrent-writer race fixed in
`0f59823` must not come back.

Note `Merge` (the `add --json` door) has the identical read-modify-write shape
per call and should be considered in the same design.

## D2 — then add the per-file extract cache

The spike's design is sound and should be reused:

**Cacheable per file** — the whole `fileResult`: nodes, edges, calls, decls,
routes, boundary hits, scopeNames.

**Never cacheable, must re-run every gather** — call resolution (module-wide
unique-name matching), route handler binding, `importresolve`, boundary
scope-by-join, `sortBatch`. These consume *all* files' output. They cost only
~0.35s, so recomputing them always is free and getting this wrong corrupts the
graph.

**Salt the key** with schema version, merged boundary rules, services, route
packs, resolutions, and **grammar-pack wasm bytes** — a rebuilt pack at the same
path changes symbol IDs and would silently reinterpret every cached `typeOf`.

Verified in the spike: one-byte change → **1 miss / 10,582 hits**; a repo rule
added → whole cache correctly invalidated (0 hits / 289 misses); a malformed
rule file → gather **fails loudly**, never serves stale cache. Content-hash
keying beats lever 1's mtime signature — `touch` correctly produced 100% hits.

## Correctness evidence, and the ADR 5 dependency

The spike could not use byte-identity naively, so it established a control
band: two *uncached* kubernetes runs already differ in **55** location/metadata
fields (ADR 5's node-id collisions). Cached vs uncached: **49** — inside the
band, with **node-ID sets and edge-tuple sets identical**. On this repo, which
is collision-free, the control band is 0 and cached vs uncached is **0 —
byte-identical**.

That is adequate evidence for a spike and **not adequate for shipping**.
⇒ **ADR 5 is a prerequisite**: until identical gathers are byte-identical,
there is no clean way to prove a cache never serves a wrong fact.

## Hazards to design against

1. **Cache size 230–268 MB per corpus** (~15–25% of repo size). Needs eviction;
   unbounded growth across branches would be hostile.
2. **Cold-gather penalty** — a CI job that gathers once and discards pays pure
   cost. Consider making the cache opt-in for one-shot runs.
3. Most entries are **negative**; caching "this file produced nothing" is what
   makes hit rates ~100%.
4. Replay order matters (node creation is first-writer-wins), which is the same
   root cause as ADR 5.

## Sequencing (the spike's recommendation, and mine)

1. **ADR 5** — stable node identity. Unblocks byte-identity as a correctness gate.
2. **D1** — incremental store write. The measured bottleneck; benefits *every*
   gather including cold ones, with no new on-disk state.
3. **D2** — per-file cache. Only then does it pay, and only then can it be
   proven correct.

## Kill criterion

If D1 lands and a one-file re-gather is already fast enough, D2 may not be
worth 250 MB of cache and a slower cold gather at all. Re-measure before
building it — the spike exists precisely so that decision is made on numbers.
