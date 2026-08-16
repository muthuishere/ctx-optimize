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

## Implemented 2026-08-15 — D1 + D3 (owner-authorised)

D1 as written, plus the one thing the draft did not settle: WHERE the content
hash is verified from. Rehashing a 2.1GB graph per lookup would cost more than
the full scan it saves, so the hash is computed by the WRITER — streamed off the
same bytes on their way to the temp file, no extra read — and recorded with the
size+mtime of the inode it describes.

That record is **`graph/index/<name>.ndjson.stamp`**, inside the index
directory, not beside the graph. The first attempt put it beside the graph and
the `--jobs 1` vs `--jobs 8` determinism gate caught it within the hour: a file
carrying an mtime cannot live in the store's transported artifact set. Under
`graph/index/` it inherits the exclusions that already exist for the index —
machine-local, out of the manifest, out of remote transport, out of the
byte-identity gate — so the store's committed artifact set is unchanged.

Read path: stat the graph, prove the stamp still describes it, compare its hash
with the index header. Never hashes. A stamp that is missing, stale, truncated
or unparseable reads as "not current" and the caller falls back — the fail-safe
property is exactly what it was.

Also landed, and it is really D2's useful half without D2's cost: the rebuild
guard gained `|| !IndexCurrent()`, so an ordinary `add` repairs a missing or
old-format index instead of leaving it dead until `add --force`. It costs four
stats when the index is already fine.

Measured after (best-of-3, load average recorded, same machine as the table
above):

| | |
|---|---|
| `card bio_split`, index alive | **6.0 ms** |
| same store, binary that cannot read the new header | 1,816 ms |
| `card bio_split` after a real incremental gather (0 nodes added) | **6.1 ms** |
| linux incremental gather, before / after the change | 56.6 s / 54.4 s (load 8–14; the hash is under the noise floor) |

Gates: the new regression test (`internal/app/indexlifecycle_test.go`) was proven
red against HEAD before the fix. `task ci` and both golden tiers green.

One thing found on the way: the big-store differential (`TestEquivalenceBigStore`)
was ALREADY failing at HEAD — 40/2001 "wrong node" — because its independently
written ground truth still used the three-tier label rank and the implementation
has had four tiers since the Flask fix. The auditor was stale, not the index; it
now mirrors the four tiers and the kernel run is 2001/2001 identical.
