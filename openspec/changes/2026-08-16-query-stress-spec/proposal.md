# ADR 29 — the stress spec: what `query` must survive before its engine changes

Status: DRAFT — the gate ADR 25 needs and does not have
Date: 2026-08-16

## Why this exists

ADR 25 rewrites the hot path of the verb this project is named for. Its gates
today are `task ci`, `task golden`, and "judged floors may not move DOWN" —
which cover CORRECTNESS on 40 questions across two corpora, and nothing else.

None of them would catch:

- a postings file that is stale against the graph it indexes
- an index that is fine on 2.85M nodes and quadratic on 6M
- a neighbour lane that is fast when the index exists and 56× slower when it
  does not, with no signal that it fell back
- a concurrent `add` while a `query` reads the index it is rewriting
- a query whose candidate set is the whole corpus because every token is common
- memory that fits a 48 GB laptop and not a 4 GB CI box

Every one of those is a way to ship "faster" and be wrong. This spec is the
list, written before the code, so the slices are built against it rather than
measured after.

## The invariants

### I1 — the answer does not change

Slice 0 (neighbour index) and slice 1 (postings) are ACCESS PATHS. For a pinned
corpus, `query --json` must be byte-identical with the index present and
absent, for every judged question and for a sample of 200 random 1–4 token
queries drawn from the corpus vocabulary.

Slices 3–4 are allowed to change the answer, and then the scoreboard is the
gate, not byte-equality.

### I2 — a stale index is never read

The index carries the size+mtime of the file it indexes (the discipline
`graph/index/*.stamp` already uses). A mismatch means FALL BACK, silently in
behaviour and loudly in `--json`:

```json
{"index": {"used": false, "reason": "stamp mismatch"}}
```

An index that is silently stale is worse than no index: it answers confidently
from a graph that no longer exists.

### I3 — the fallback is a fact, not a mystery

Every slice that adds an index must answer "did it get used?" without a
profiler. `query --json` carries which lanes were served from an index. The
56× neighbour result means a silent fallback is a 2.3-second regression that
looks like normal slowness.

### I4 — concurrent gather and query never corrupt an answer

`add --force` rewrites the graph and the index while a `query` may be reading
them. The store already writes by atomic rename; the spec is that a reader
either sees the whole old index or the whole new one, never a torn read, and
never a new index against an old graph.

Tested by running a gather loop against a query loop for 60s and asserting
every answer is well-formed and matches one of the two valid graphs.

### I5 — degenerate queries are bounded

A query whose every token is corpus-wide (`the get set value`) selects most of
the corpus. Postings must not turn that into a slower path than today's scan.
The spec: candidate generation is capped, and when the cap binds, the answer
says so rather than silently truncating.

Measured on linux: `df max = 1,725,630` — one token already covers 61% of all
nodes. This is not hypothetical.

### I6 — memory is bounded and stated

Today `query` holds all nodes, all edges and an adjacency map — measured 2.85M
nodes and 5.54M edges on linux. The spec: report peak RSS for the pinned
corpora, and slice 0 must LOWER it (the adjacency map stops existing), not
merely move time around.

### I7 — the index does not outgrow the graph

Postings on linux project to ~88.7 MB against an index directory already at
631 MB and a store at 2.0 GB. The spec: index bytes are reported per corpus
and per slice, and a slice that more than doubles the index directory needs a
stated reason.

## The stress corpora

Not new downloads — the arena and golden corpora that exist:

| corpus | scale | what it stresses |
|---|---|---|
| `~/ctx-golden-corpora/linux` | 2.85M nodes, 5.54M edges | the ceiling; every phase |
| `~/ctx-golden-corpora/Newtonsoft.Json` | small, dense | pinned judged floor |
| this repo | 5.5k nodes | the fast path most users see |
| a synthetic hub | one node with 100k edges | adjacency blow-up, cap behaviour |

The synthetic hub is the only thing to build, and it exists to test I5/I6 where
a real corpus is merely large rather than pathological.

## The runs

1. **Equivalence** (I1) — index on/off, byte-diff `--json`, all judged + 200
   random queries per corpus.
2. **Staleness** (I2) — gather, query, mutate the graph behind the index,
   query again; assert fallback and the disclosure field.
3. **Concurrency** (I4) — 60s of `add --force` against continuous `query`.
4. **Degenerate** (I5) — the ten most common tokens per corpus, alone and in
   pairs; assert bounded time and a stated cap.
5. **Budget** (I6/I7) — peak RSS and index bytes, per corpus, per slice,
   recorded in the results file the way the linux-scale row already is.

## What this spec is NOT

- Not a competitor benchmark. That is `benchmarks/`, and its rules already
  exist (reuse the arena; pinned refs; say "unpinned" until `versions.json`).
- Not a replacement for `task golden`. Correctness stays there; this is
  everything golden does not look at.
- Not a promise of numbers. It says what must be MEASURED and what must not
  regress; the values land in the results file, not here.

## Kill criterion

If a slice cannot satisfy I1 and I2, it does not ship regardless of its
speedup. An access path that changes answers, or reads a stale index, converts
this project's central claim — parsed fact you can cite — into a fast guess.
