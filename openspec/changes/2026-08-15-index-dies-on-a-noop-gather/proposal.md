# ADR 18 — the lookup index dies on a gather that changed nothing

Status: DRAFT — owner review pending 2026-08-15. No product code until agreed.
Scope: `internal/store/index.go` staleness header, and the rebuild guard at
`internal/app/multimodule.go:929`. No schema change, no producer change, no new
verb.
Found by spiking the "why is CodeGraph's query faster than ours" question and
checking, before optimising anything, whether we were using the index we
already ship. We were not.

## The headline

**`card` on the Linux kernel is 6 ms with the index and 1,629 ms without it —
and an incremental gather that adds zero nodes is enough to lose it.** Measured
on this machine today, best-of-3 each:

| state | `card bio_split` |
|---|---|
| store gathered before the index existed | 1,708 ms |
| after `add --force` (index built, 601 MB) | **6 ms** |
| after one incremental `add` that added 0 nodes | 1,629 ms |

The published claim — "card on the Linux kernel went 1.8 s to under 20 ms" — is
true, and it survives exactly until the next gather.

## It is not a stale-store problem. It is every store.

Every large store on this machine was unindexed before this spike:

| store | graph | index files |
|---|---|---|
| linux | 2.0 GB | 0 |
| k8s | 375 MB | 0 |
| cpython | 151 MB | 0 |
| springboot | 124 MB | 0 |

And nothing repairs it. Measured on a small store by deleting `index/` and
running each verb:

| verb | rebuilds the index? |
|---|---|
| `add` (no change) | no |
| `add` (real source edit) | no |
| `sync` | no |
| `up` | no |
| `add --force` | **yes** |

`up` is the verb our own instructions call the front door — "bootstrap, gather,
or pull, and a safe no-op ever after". It cannot fix this, and nothing tells the
user there is anything to fix.

## Root cause: two definitions of "changed" that disagree

The rebuild guard is `if graphChanged || force` (multimodule.go:929) and
`graphChanged` means *the node set moved*. The staleness header is
`size=%d mtime=%d` of the source file (index.go:139).

The store rewrites `nodes.ndjson` / `edges.ndjson` on **every** gather, whether
or not the content moved. Proven by file times on the kernel store: the graph
was rewritten at 21:33:10, the index files still carry 21:28:14 from the earlier
forced build, and the gather log reads `added 0 nodes`.

So a no-op gather rewrites the file, the modtime changes, the header no longer
matches, and the reader correctly declares the index stale — while the guard,
looking at a different definition of change, correctly decides there is nothing
to rebuild. **Both halves behave exactly as written; together they guarantee the
index is dead after the first gather.**

This is the ADR-16 shape again: a computation whose two sides are keyed on
different things, so the join can never hold.

## Why it went unnoticed

The index fails safe by design — a stale index costs speed, never correctness —
so there is no wrong answer, no error, and no log line. The only symptom is that
the fastest verb in the product silently runs at its old speed. Our own golden
perf gate measures **gather**, not lookup, so nothing was watching.

## D1 — key the header on content, not on modtime

The manifest already content-hashes the graph. If the index header carries that
same hash instead of size+mtime, a rewrite that changed nothing does not
invalidate anything, and the two definitions of "changed" become one. Cost is a
hash the store already computes.

## D2 — rebuild when the file is rewritten, not when nodes move

Smaller and independent of D1: widen the guard so a graph rewrite rebuilds the
index. Correct, but it pays ~2.5 s on every kernel gather for a rebuild that is
usually unnecessary. **D1 is the better fix; D2 is the fallback if the hash is
not cheaply available at that point in the write path.**

## D3 — say so when a lookup falls back

Whatever lands, a stale or missing index should be visible. `status` should
report index state, and `card` should be able to say it answered from a full
scan. A silent 270x regression is the part that let this ship.

## Gates

- A test that builds a store, asserts the index resolves, runs a no-op gather,
  and asserts it STILL resolves. Prove it red against today's binary first —
  this repo has a standing lesson that a gate which records what it measures is
  not a gate.
- The differential test that already compares indexed and fallback resolution
  (2002/2002 identical on the kernel) must still pass.
- Kernel `card` stays in single-digit milliseconds after an incremental gather.

## What this changes about the roadmap

ADR 12 D3 proposed a query index because CodeGraph answers kernel queries in
0.79 s to our 3.70 s. That gap is real and worth closing. But `card` is the verb
an agent calls repeatedly in a session, and with the index alive it answers in
**6 ms** — two orders of magnitude under their query. Fixing what we already
built comes before building more.
