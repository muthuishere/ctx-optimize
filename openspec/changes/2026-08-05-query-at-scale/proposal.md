# ADR — an access path for the graph: index lookups instead of full materialization

Status: **DRAFT** — 2026-08-05. Owner-directed after the linux head-to-head
("we do something about it… we will do some spikes"). Measurements in
`spikes.md`; every number below traces there.

## Context — we win the build and lose the lookup

Measured 2026-08-05 on linux v6.9 (Apple M5 Pro, 18 cores):

| | ctx-optimize | CodeGraph 1.5.0 | graphify 0.9.12 | GitNexus 1.6.9 |
|---|---:|---:|---:|---:|
| cold gather | **118.18s** | 289.86s | 527.72s | did not finish in 45 min |
| symbol query | **4,039ms** | **536ms** | 22,799ms | — |
| nodes | **2,849,719** | 1,838,442 | 910,778 | — |
| store | **2.0GB** | 4.1GB | 3.1GB | — |

We build 2.5x faster than the next tool and emit 55% more nodes in half the
disk — then lose the lookup by 7.5x. That single row is what stops
"the fastest code graph" from being true.

The cause is not representation and not I/O. Reading all 2.1GB costs **0.12s**;
`json.Unmarshal` of the node file costs **3.19s**; a byte scan of the same bytes
costs **0.180s**. So ~97% of a query is deserialization — and it is paid
*before the question is known*, because every verb materializes the entire graph
first. `analyze.Resolve` (`internal/analyze/analyze.go:46`) takes
`nodes []schema.Node` — an already-loaded slice. `Store.Nodes()`
(`internal/store/store.go:144`) is the only way to get one, and it parses all
2.85M records. There is no access path. CodeGraph's 4.1GB is 54% B-tree index;
they spend disk so they never read the file.

## Decision

Add a **derived, fail-safe index** beside the graph, and give the hot verbs a
lookup API that does not materialize the graph.

1. **`<store>/graph/index/`** — plain sorted text, git-diffable, rebuilt from the
   graph like every other derived artifact:
   - `labels.idx` — `label\toffset[\toffset...]` , one line per distinct label
   - `edges-by-source.idx`, `edges-by-target.idx` — same shape, keyed by endpoint
   Measured: **432MB / ~2.5s** for linux; **0.6MB / 0.008s** for a 1,476-file
   repo; **0.1MB / 0.002s** for a 344-file one. Against CodeGraph's 2,201MB of
   B-tree, 5x smaller.

2. **Lookup by mmap + binary search**, then parse only the lines that matched.
   Measured **0.0001s** for an exact label on the linux store, against 3.19s
   today — and against CodeGraph's 536ms.

3. **The index is an optimization that fails safe.** Its header records the
   graph file's size and modtime. On ANY mismatch — absent, stale, truncated,
   unparseable — the caller silently falls back to the existing full-scan path.
   The index can never make an answer wrong, only fast. This is the same
   discipline as the 0.12.0 wiki work: we may say "slower", never "wrong".

4. **Sets, never first hits.** `labels.idx` maps a label to ALL its offsets.
   20.3% of linux records sit under a non-unique label (`"description"` alone
   has 14,989 nodes; 4,071 labels appear 10+ times). A one-offset index would
   silently drop them — an under-report that reads as a complete answer, which
   is the failure this project exists to prevent.

5. **Escape-aware key extraction is mandatory.** The naive scan (bytes between
   `"label":"` and the next quote) is WRONG twice: it truncates at an escaped
   quote, and returns encoded bytes so `\t` never becomes a tab. **11,798 linux
   labels (0.414%) contain an escape**, and the spike's first equivalence run
   failed with **76 mismatches in 20,031 labels** because of it. The extractor
   walks escapes and decodes only when one is present — 99.6% keep the
   zero-allocation path.

## Scope — deliberately one verb

**In:** the index builder, the lookup API, gather-time build, fail-safe
fallback, and **`card` only** as the first consumer. `card` is the slowest verb
at scale (7.5s on linux, because it loads nodes AND edges) and the one whose
whole pitch is "signature + callers, no file read".

**Out, explicitly:**
- **Free-text `query`.** It runs lexical IDF + prefix + trigram ranking over
  every node; a label index does nothing for it. CodeGraph carries FTS5 plus a
  150MB `name_segment_vocab` for this. A postings index is a separate ADR and is
  **unmeasured** — no claim about `query` latency may be made from this work.
- `change-plan`, `affected`, `path`, `hubs` — they become trivial follow-ups once
  the API exists, but each is its own equivalence proof.
- SIMD / `simdjson-go`. Settled by measurement: the byte-scan ceiling is 0.180s
  and the index is 0.0001s, 1,800x below it. A constant factor cannot beat an
  access path. ripgrep — the best SIMD scanner there is — takes 3,022ms on this
  tree against CodeGraph's indexed 536ms. Revisit only for whole-graph verbs.

## The gate — equivalence, permanently

The speedup ships only behind a proof that it changes no answer:

- A golden-tier test builds ground truth by full parse, resolves the same labels
  through the index, and compares **whole result sets field by field**. Zero
  mismatches or the build fails. This is the check that caught the escape bug;
  it stays.
- Existing judged floors (linux-block 16.5, newtonsoft 13.0) must not move. This
  change is additive, so a moved score means something broke.
- A perf pin: `card` on the pinned corpus must not regress.

## Risks

- **Staleness is the real hazard**, and it is the wiki lesson again. Producer-
  scoped `Replace` rewrites the graph; an index left behind would answer from a
  previous tree. Mitigated by the size+modtime header and fail-safe fallback —
  the index is never trusted, always verified.
- **Gather cost grows ~2.1%** at kernel scale (118.18s → ~120.7s). Within the
  run-to-run variance already observed (118s vs 164s on the same tree, ~30%),
  and repaid after a single query.
- **Disk grows ~21%** on linux (2.0GB → 2.43GB). Still well under CodeGraph's
  4.1GB for a bigger graph.
- Crash-safety and concurrent gathers are unaddressed; the index is written by
  atomic rename like every other store artifact, and a torn index fails the
  header check and falls back.

## Not claimed

- Nothing about free-text `query` latency.
- One corpus, one machine. Absolute numbers vary ~30% with page-cache state;
  the ratios are the finding.
- No claim that this makes us "fastest" outright — after this work the honest
  claim is **fastest to build, and instant on symbol lookup**.
