# Spikes — why `card` costs 4s on linux, and what actually fixes it

Run 2026-08-05, `v0.12.0`, Apple M5 Pro (18 cores, 48 GB), against the real
linux v6.9 store gathered by this binary: `graph/nodes.ndjson` 879 MB /
2,849,719 records, `graph/edges.ndjson` 1.2 GB / ~5.54M records.

Spike source: `scratchpad/spike-index/main.go` (throwaway; the numbers are the
artifact). Every figure below was produced by a command run on this machine.

## Why this exists

CodeGraph 1.5.0 answers a kernel query in **536 ms**. We take **4,039 ms** —
7.5x slower — while *winning* cold gather 118s to 290s. Their store is 4.1 GB
against our 2.0 GB, so the usual "they're bigger because they store more"
explanation is backwards: they are bigger *and* faster.

`dbstat` on their SQLite file says exactly where the bytes go:

```
INDEX        2,201 MB   ← 54% of the database
DATA         1,655 MB
FTS/SEARCH     339 MB
```

Their *data* is 1.65 GB holding 1.84M nodes / 5.82M edges. Our 2.0 GB holds
2.85M nodes / 5.54M edges — **we store more graph in less space**. The
difference is not representation. It is that they built an access path and we
did not: `idx_nodes_name`, `idx_nodes_lower_name`, `idx_nodes_qualified_name`,
`idx_edges_source_kind`, `idx_edges_target_kind`, `idx_edges_identity`
(662 MB on its own), plus an FTS5 index over node names.

## S1 — the cost is JSON, not I/O and not the disk

| what | time |
|---|---:|
| `cat nodes.ndjson edges.ndjson > /dev/null` (2.1 GB, warm) | **0.12 s** |
| `wc -l` both files (SIMD newline count, no parse) | **1.25 s** |
| full `json.Unmarshal` of nodes.ndjson, parallel across 18 cores | **3.19 s** |
| mmap + hand-rolled byte scan of nodes.ndjson, extract label, no JSON | **0.180 s** |

`encoding/json` costs **17.7x** what touching the same bytes costs
(3.19s vs 0.180s). Reading the file is 0.12s. So the 4-second query is
essentially all deserialization, and it is paid *before the question is even
known* — every verb materializes 8.4M records to answer about one symbol.

## S2 — the SIMD question, settled by measurement

The owner asked whether SIMD / faster parsing is the fix. Bound it from below
rather than argue: S1's byte-scan row **is** the ceiling for any scan-based
design — mmap, one pass, `IndexByte`, zero allocation per record, no parser at
all. Nothing vectorised can beat "look at every byte and do almost nothing."

That ceiling is **0.180 s** for nodes.ndjson.

Two consequences, and they point in opposite directions:

1. **SIMD is not needed to beat CodeGraph.** 0.180s already beats their 536ms
   by ~3x. Simply not using `encoding/json` on the hot path is worth 17x.
2. **SIMD cannot beat an index.** S4 below is 0.0001s — 1,800x below the scan
   ceiling. A constant-factor win on an O(N) design loses to an O(log N) one,
   always. Confirmed independently in the field: ripgrep, the most optimised
   SIMD scanner in existence, takes **3,022 ms** on this tree, against
   CodeGraph's indexed 536 ms.

## S3 — building the index

One pass over nodes.ndjson extracting `label -> byte offset`, sorted, written
as a plain `label\toffset\n` text file.

| | |
|---|---:|
| build time | **0.958 s** |
| entries | 2,849,719 |
| size on disk | **103.0 MB** |

Two numbers worth holding together: the index costs **0.8% of a 118s gather**,
and it is **21x smaller than CodeGraph's 2,201 MB of B-tree** while serving the
same lookup. We do not need their index set — we need one of them.

It also stays inside the doctrine: a sorted text file is plain, `cat`-able,
git-diffable, and rebuilt from the graph like every other derived artifact. No
database, no CGO, no dependency. **This is the Go advantage, and it is not
speed** — Node has no comfortable path to mmap + binary search over a custom
file, which is why CodeGraph had to adopt SQLite and pay 2.2 GB for it.

## S4 — the lookup that replaces the scan

mmap the sorted index, binary search by line, seek that one offset in
nodes.ndjson, `json.Unmarshal` exactly one line.

```
S4  mmap + binary search + 1 parse   0.0001s  ok=true
    unimac_mdio_probe.unimac_mdio_pdata [struct]
    drivers/net/mdio/mdio-bcm-unimac.c L238-L238
```

**0.0001 s**, verified against a real symbol with correct kind, file and line
(the first run of this spike reported 0.0001s with `ok=false` — a not-found
fast path that proves nothing; the number above is a resolved hit).

| | time | vs today |
|---|---:|---:|
| today (S1 full parse) | 3.19 s | 1x |
| scan ceiling (S2) | 0.180 s | 17.7x |
| CodeGraph 1.5.0 | 0.536 s | 6.0x |
| **index lookup (S4)** | **0.0001 s** | **~32,000x** |

## What this does NOT cover — read before scoping the ADR

- **Only exact-label resolution is measured.** That is the hot path for `card`,
  `change-plan`, `affected` and `path` — Resolve is `id > label > fuzzy`. It is
  NOT what free-text `query` does: that runs lexical IDF + prefix + trigram
  ranking over every node, which a label index does not serve. CodeGraph has an
  FTS5 index and a 150 MB `name_segment_vocab` for precisely this. A postings
  index for `query` is a separate, larger question and is unmeasured here.
- **Edges are not indexed in this spike.** `card`'s caller/callee lists need
  edges.ndjson (1.2 GB), so a complete `card` needs an edge index keyed by
  source and by target, or it pays a 0.25s-class scan. Unmeasured.
- **No process-startup cost is included.** These are in-process timings; the
  CLI adds its own fixed overhead.
- **Ambiguity is unaddressed.** Labels are not unique (the store already
  reports AMBIGUOUS shortlists), so the index must map a label to *many*
  offsets. The spike keeps duplicate entries sorted by `(label, offset)` and
  returns the first hit — a real implementation has to return the set.
- One corpus, one machine, one gather. The 118s vs the earlier 164s figure for
  the same tree shows run-to-run variance of ~30% from page-cache state alone;
  treat all absolute numbers as order-of-magnitude, and the *ratios* as the
  finding.

---

# Spike round 2 — the QUALITY gates

Round 1 proved ~32,000x on exact-label lookup. That number is worthless if it
changes one answer, so round 2 attacks the three ways it could. Same machine,
same linux store. Spike source: `scratchpad/spike-index/q/main.go`.

## Q1 — labels are NOT unique, and a first-hit index would be a disaster

| | |
|---|---:|
| records | 2,849,719 |
| distinct labels | 2,383,591 |
| **records under a NON-UNIQUE label** | **578,402 (20.3%)** |
| worst label | `"description"` -> **14,989 nodes** |

Multiplicity: 2,271,317 labels appear once; 84,111 twice; 11,405 three times;
**4,071 labels appear 10+ times.**

**Consequence, and it is the whole design:** the index must map a label to a
SET of offsets. A `label -> one offset` index would silently drop 20.3% of
records — returning one `description` node out of 14,989, one caller out of
many — which reads as a complete answer while being an under-report. That is
the wrong-not-absent failure this project exists to prevent, and it is the same
failure mode as the stale wiki we removed in 0.12.0.

## Q2 — edges index cleanly, in both directions

`card` needs callers AND callees, so edges must be reachable by source and by
target without scanning 1.2 GB.

| | |
|---|---:|
| edges indexed | 5,539,123 |
| build time | **1.55 s** |
| by-source | 788,067 keys, 98.1 MB |
| by-target | 2,782,380 keys, 230.9 MB |
| **combined** | **329.0 MB** |

Both are plain sorted `key\toffset\toffset...\n` text.

Full index footprint = 103 MB (labels) + 329 MB (edges) = **432 MB**, against
CodeGraph's **2,201 MB** of B-tree. Still 5x smaller, serving the same lookups.
Total build cost = 0.958s + 1.55s = **~2.5 s added to a 118 s gather (2.1%)**.

## Q3 — the gate: is the indexed answer identical to the parsed answer?

Ground truth built by full `json.Unmarshal` of every record, then 20,031 labels
sampled deterministically (every Nth of the sorted label set) and resolved
through the index, comparing the FULL SET of nodes per label field by field.

**First run: 76 MISMATCHES.** The spike found a real correctness bug in its own
fast path, which is the entire reason to run it.

Cause: the naive extractor takes the bytes between `"label":"` and the next
quote. That is wrong twice — it stops at an *escaped* quote (`\"`), truncating
the value, and it returns the *encoded* bytes, so `\t` stays backslash-t and
`<` never becomes `<`. Measured: **11,798 linux labels (0.414%) contain a
JSON escape.** Examples: `cflags-$(CONFIG_CPU_R5000)\t+`, `INSTALL\t\t?`,
`<URL`.

A mis-keyed index does not raise an error. It fails to find the symbol, so
`card` reports fewer callers and `affected` reports a smaller blast radius —
silently, on 0.4% of symbols.

Fix: walk the string respecting backslash escapes to find the true end, then
decode properly ONLY when an escape is present. 99.6% of labels keep the
zero-allocation fast path, and the extraction pass still runs in 0.8 s.

**Second run:**

```
Q1  labels: 2383591 distinct / 2849719 records   (was 2383588 — the bug
                                                  corrupted the COUNT too)
Q3  ground truth built by full parse: 2383591 labels
Q3  checked 20031 labels (899 of them non-unique)
Q3  RESULT: 0 mismatches — indexed path is byte-identical to full parse
```

The distinct-label count now agrees with the full parse exactly. It did not
before, and nothing but this equivalence check would have caught that.

## What round 2 establishes for the ADR

1. **Equivalence must be a permanent gate, not a one-off.** This exact
   comparison — full-parse ground truth vs indexed path, whole result sets —
   belongs in the golden tier, run on every corpus. The speedup is only
   shippable behind it.
2. **Sets, never first hits.** 20.3% of records sit under a non-unique label.
3. **The byte-scan fast path needs an escape-aware extractor**, and the cost of
   correctness is ~0 because escapes are rare.
4. Footprint and build cost are settled: **432 MB, ~2.5 s**, plain sorted text,
   git-diffable, no database.

## Still NOT measured

- **Free-text `query`** (lexical IDF + prefix + trigram) is untouched by a label
  index. CodeGraph carries FTS5 + a 150 MB `name_segment_vocab` for this. A
  postings index is a separate, larger question.
- **Index staleness.** The graph is rebuilt by producer-scoped `Replace`; the
  index must be invalidated with it or it becomes a stale wiki with worse
  consequences. Unaddressed here.
- Concurrency, partial writes, and crash-safety of the index files.
- One corpus, one machine. Ratios are the finding; absolutes vary ~30% with
  page-cache state.

---

# Round 3 — IMPLEMENTATION measurements (not the spike's)

The spike used `mmap`. The shipped code cannot: mmap is not portable stdlib and
this binary targets Windows too, so lookups use `ReadAt` over small windows.
That difference is real and the numbers below are the ones to quote — the
spike's 0.0001s is NOT what the implementation delivers.

Measured by `internal/store/index_scale_test.go` (env-gated:
`CTX_OPTIMIZE_TEST_BIGSTORE=<store> go test ./internal/store/ -run Scale -v`)
against the real linux store, 2,849,719 nodes / 2.10 GB graph.

| | value |
|---|---:|
| index size | **0.43 GB (20% of the graph)** |
| build time | **5.99 s** |
| total allocated during build | 4.47 GB |
| **peak heap (Sys)** | **2.83 GB** |
| **lookup, mean of 200 spread across the store** | **1.61 ms** |

## Two defects this round caught

**1. The first implementation read the ENTIRE index on every lookup.**
`loadIndex` used `os.ReadFile`, pulling 103 MB (230 MB for edges) into memory
per call. Measured **6.98 ms/lookup**. Replaced with an open handle plus binary
search over 8 KB `ReadAt` windows: **1.61 ms**, a 4.3x fix. Nothing but the
scale test would have found this — every unit test passes either way, because
correctness was never affected. Speed defects are invisible to correctness
tests, which is the argument for keeping this test.

**2. Build cost is 5.99 s, not the spike's 2.5 s.** The spike mmap'd and
extracted one field; the implementation reads both files, handles escapes, and
writes three indexes atomically. Still ~5% of a 118 s kernel gather, and it is
the honest number.

## Honest position vs the field

| | linux symbol lookup |
|---|---:|
| ctx-optimize today (full parse) | 4,039 ms |
| **ctx-optimize with index** | **1.61 ms** |
| CodeGraph 1.5.0 (SQLite B-tree) | 536 ms |
| ripgrep (different question) | 3,022 ms |

**~2,500x faster than today and ~330x faster than CodeGraph**, at 20% of the
graph size where their index is 54% of their database. The spike's 32,000x is
not achievable portably; 2,500x is, and it is more than enough to take the
lookup axis.

## SCALABILITY LIMIT — the honest ceiling

`BuildIndex` holds the whole source file in memory (`os.ReadFile`) plus a
`key -> []offset` map while building. Peak heap was **2.83 GB for a 2.10 GB
graph — roughly 1.35x the graph size.**

That is fine for linux on a 48 GB machine and fine for every normal repo. It is
NOT fine indefinitely: a graph 10x this size would want ~28 GB of RAM, and CI
runners and laptops have far less. **This is a known ceiling, not a solved
problem.** Mitigations exist and none are implemented: stream the scan instead
of `ReadFile`, build with an external merge sort, or shard the index. Whoever
takes the next repo past ~5M nodes should expect to do that work first, and
should not discover it in production.

The failure mode is at least loud and safe: an OOM kills the gather rather than
producing a wrong index, and if the index is absent every lookup falls back to
the full scan.

---

# Round 4 — WIRED AND VERIFIED (`card`, linux)

`card` now takes the index when it can prove the answer identical. Measured on
the real kernel store, `./bin/ctx-optimize card <sym> --path ~/…/linux`:

| symbol | resolves via | with index | without index |
|---|---|---:|---:|
| `unimac_mdio_probe.unimac_mdio_pdata` | exact-label | **0.00s** | 1.73s |
| `additionalProperties` (3,862 nodes share it) | exact-label | **0.02s** | 1.83s |
| `blk_mq_init_queue` | **fuzzy → refuses** | 7.45s | 7.40s |

Output is **byte-identical** between the two paths (`diff` clean), including the
3,862-node shared-label case.

## The honest boundary

The speedup applies to **exact-id and exact-label resolution only**. The third
row is not a failure — `blk_mq_init_queue` is not a label in this store, so
ResolveVia falls to fuzzy, finds several near names scoring alike, and REFUSES
with candidates. Ranking and tie-detection read every node by definition, so
there is nothing to index. `cardViaIndex` returns ok=false and the unchanged
full path runs.

So: **exact hits are ~100x faster; misses, fuzzy hits and ambiguity cost what
they always did.** Any published claim must say which.

## The gate caught a real bug

`TestEquivalenceBigStore` FAILED its first run on the kernel store:
**21 wrong nodes in 2,002 resolutions.**

Cause: that store had `labels.idx` but no `ids.idx` (built before the id index
existed), and `IndexCurrent()` only checked the labels file. It reported
"current", so `NodeByID` took its full-scan fallback on every lookup — the
"fast" path quietly loading 2.85M nodes, ~1s per call instead of ~2ms, and
diverging under the memory churn.

Fix: `IndexCurrent()` requires EVERY node index, not just one. Partial is not
current. After it, the same check passes clean:

```
checked 2002 resolutions, 2002 edge sets
--- PASS: TestEquivalenceBigStore (6.64s)
```

It also went from 181s to 6.6s, because the fallback was what made it slow.

## Cost of the index at small scale

Re-measured after wiring, cold gather best-of-3:

| corpus | before | after | delta |
|---|---:|---:|---:|
| corpus-ctx-src 409 files | 0.326s | 0.341s | +4.6% |
| corpus-graphify-src 1,476 files | 0.648s | 0.685s | +5.7% |

Free-text `query` is unchanged (13ms / 28ms) — it does not use the index and
this work never claimed it would.

## Gates green

`task ci`, `task golden` hermetic + corpus, judged floors unmoved (linux-block
16.5, newtonsoft 13.0), plus the new differential equivalence check at both
hermetic and corpus tiers.
