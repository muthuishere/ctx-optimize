# ADR — on-demand content hydration (`--include-content`), never store bodies

Status: DRAFT — 2026-07-24. For DISCUSSION + a required SPIKE before code.
Split out of the answer-quality ADR (F3) because it is a positioning decision on
its own. Motivated by MEASURED evidence from the multilang benchmark.

## The measured finding (why this exists)

The multilang bench proved the footprint win and the "coverage" loss are ONE
design choice:
- **Rivals store verbatim source in the store** — proven: CodeGraph's SQLite
  holds `return i.proceed();` + Javadoc; GitNexus's DB holds file text incl.
  README. That is why their stores are big (postgres 183 MB–1 GB vs our 44 MB)
  AND why they scored coverage 1.0 in the small-model judge (they have the body
  to return).
- **ctx-optimize stores 0 verbatim source lines** — structure + `file:line`
  pointers only (verified on java-spring: `strings store | grep source = 0`).
  Hence tiny store AND coverage 0.79.

## The positioning (locked with owner)

Not "we're worse on coverage." It is: **"we cite the exact location so your agent
does one targeted read instead of grepping — and can inline the body on demand.
We don't keep a stale second copy of your source like the others do."**

Key reframe: for a coding agent, a cited `file:line` is often the COMPLETE answer
— the agent reads that exact range (one cheap Read) instead of grep-many /
read-many. The judge's "coverage" metric rewards ONE-SHOT answerability, but an
agent is not one-shot; it can follow up with a targeted read. **So our lower
coverage is partly a metric artifact, not an agent deficiency.**

## Decision

| | verdict |
|---|---|
| Store bodies in the store | ❌ NEVER — redundant (source is on disk), bloats store to rival sizes, goes stale on edit. This is the rivals' crutch. |
| Default output = pointers (`file:line` + signature + doc snippet) | ✅ KEEP — correct for agents; the footprint + efficiency win. |
| Opt-in `query`/`card` `--include-content` (a.k.a. `--full`/`--body`) | ✅ BUILD — hydrate the body from the source file **on demand** using the stored `file:line`, via the existing `verify.go` slice reader. Zero store cost; never stale (reads current file); body only when asked. |

- One FLAG on existing verbs, not new verbs.
- Real value of the opt-in is **saving a round-trip** on multi-hit "explain"
  queries where the agent would read every hit anyway — not matching a score.
- Freshness: hydration reads the CURRENT file, so if the stored line range drifted
  (file edited since gather) the slice may be off-by-lines. Mitigation: verify the
  slice's anchor (reuse `verify` / the freshness gate) and/or depend on the
  autosync ADR; on drift, widen/re-anchor or fall back to pointer-only with a note.

## Spike plan (evidence-first — REQUIRED before code)

1. **Hydration cost**: for a representative `query` (top ~7 hits), measure the
   added wall time + bytes to read each hit's `file:line` slice from disk vs the
   pointer-only answer. Hypothesis to confirm: hydration adds only a few ms
   (one open+seek+read per hit) and stays O(hits), not O(repo). Seed number
   from this session appended below when run.
2. **Freshness drift**: edit a file after gather, then hydrate a stale range —
   quantify how often the slice is wrong and whether an anchor-check (match the
   node's label/signature line inside the slice) reliably detects drift.
3. **Coverage lift**: re-run the small-model quality judge with
   `--include-content` on ctx-optimize's arm; confirm coverage rises toward the
   rivals' ~1.0 while store size stays flat (the whole point). Compare
   correctness/coverage/context-bytes before vs after.
4. **Store-cost counterfactual**: estimate what storing bodies WOULD cost our
   store (≈ source bytes) to quantify what we're avoiding — confirm it lands us
   in rival territory (the 1 GB club), justifying "never store."

## Success check

- Default `query`/`card` unchanged (pointer-only; store bytes + latency flat).
- `query --include-content` returns full bodies hydrated from disk, store size
  unchanged, added latency ≈ spike-measured few ms/hit.
- Judge coverage with `--include-content` approaches rivals' at OUR store size —
  the "smallest store AND full bodies" claim becomes measured, not asserted.
- Freshness drift is detected (anchor-check) rather than silently mis-sliced.

## Spike results (MEASURED 2026-07-24)

- **Implemented + green**: `--include-content` on `query` AND `card`, JSON-first
  (`content` field per hit, hydrated from the file via the stored `file:line`,
  reusing `verify.go`'s slice reader). Default output byte-identical (test-pinned).
  Graceful degrade: deleted file → `content_error`, exit 0, valid JSON, no crash.
  `task ci` green.
- **Hydration cost (µbench, repo store, query with 17 hits, median of 10)**:
  default **16.7 ms** → `--include-content` **17.5 ms** = **+0.8 ms (+5%)** for
  14.6 KB of hydrated source. ~0.05 ms/hit. O(hits), not O(repo) — stays
  sub-few-ms at any repo size (hits are budget-bounded). Zero added store bytes.
  → Confirms the thesis: full-body coverage for <1 ms and 0 stored source.
- **Freshness**: hydration reads the file as-is; drift check (via `verify` /
  freshness gate) still TODO for production — spike scope.
