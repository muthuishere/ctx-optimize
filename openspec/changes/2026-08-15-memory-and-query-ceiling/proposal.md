# ADR 12 — two ceilings with identified causes: RSS per module, query per byte

Status: D0 IMPLEMENTED 2026-08-15 (owner: "it's okay in my laptop for full
throttle for others we can have differently" — exactly what GOMAXPROCS gives:
unchanged on a laptop, automatically bounded in a container). reqsume 7 modules
now 0.73GB at GOMAXPROCS=2 / 2.66GB at 6 / 9.40GB at 18, where all three were
~12.4GB before. D1 IMPLEMENTED (process-wide worker budget): reqsume 9.40 -> 3.42GB at full
throttle, -64%, byte-identical, single-module untouched.
D2 (the 64MB floor) is DEFERRED ON EVIDENCE: marginal cost is ~160MB per worker
while the initial-memory floor is only 64MB of it, so D2 cannot close the
remaining gap. Bounding the guest out_buf is the real lever — own ADR.
Ceiling 2 (query index) remains DRAFT.
Scope: `internal/extract/code` (instance pooling) and `internal/query` (index).
Both causes are MEASURED, not inferred.

## Ceiling 1 — peak RSS scales with MODULES, not repo size

| repo | modules | files | peak RSS |
|---|---|---|---|
| reqsume | **7** | 4,883 | **10.58 GB** |
| ctx-optimize | 1 | ~1,200 | 2.69 GB |
| go-kubernetes | 1 | 24,471 | 4.46 GB |
| linux | 1 | 145,250 | 11.11 GB |

reqsume is a *small* repo and outweighs kubernetes 5× at a fifth of the files.
graphify uses **429 MB** on the same reqsume tree — **25× less**.

**Cause.** Workers are `runtime.NumCPU() - 1` (`code.go:420`) = 17 here. Each
worker builds its own wazero instance (`code.go:449`), and an instance starts
with a **64 MB** linear memory (ADR 8 measured this: 71% of an 8.3 GB gather's
allocation was wazero linear memory). Modules gather in PARALLEL. So the live
footprint is:

    modules_in_flight x (NumCPU-1) x 64 MB

7 x 17 x 64 MB = **7.6 GB** of guest memory alive at once, before the graph.
That matches the 10.58 GB observed, and it explains why a small monorepo is
the worst case rather than a big single-module repo.

**It is NOT the markdown swap, the boundary lane, or importresolve — proven by
A/B.** Same module (`reqsume/apps/api`), same machine:

| build | peak RSS |
|---|---|
| baseline `0a2b192` — regex markdown, no boundary lane, pre-D7 | **2.56 GB** |
| HEAD — goldmark + boundary + D7 + services + drift | **2.57 GB** |

0.01 GB apart. Every lane this session added is memory-free; the footprint is
entirely the wasm instance model and has been there all along. Scaling check on
the same repo: 1 module 2.57 GB → 7 modules 12.37 GB (~1.77 GB per module),
which is the per-module signature, not a per-lane one.

**Why it matters now, not later:** an 8 GB CI runner cannot gather reqsume, and
a 16-core laptop gathering a 10-module monorepo would need ~10 GB. This is a
correctness-of-experience bug for exactly the multi-module users the product
targets.

### D0 — key on GOMAXPROCS, not NumCPU (one line, do this first)

The owner's instinct — "small CI runners allocate few CPUs, so we scale down
automatically" — is right for a small **VM** and wrong for a **container**.

`code.go:420` uses `runtime.NumCPU()`, which ignores `GOMAXPROCS` **and ignores
cgroup CPU quotas**. Measured: `GOMAXPROCS=2` moved peak RSS only 2.57 → 1.91 GB
(−26%), not the ~8× a real drop from 17 workers to 1 would give — because we
still CREATE 17 instances and GOMAXPROCS merely limits how many run at once.
Confirmed directly: `NumCPU=18 GOMAXPROCS=3` under `GOMAXPROCS=3`.

Consequences:
- 2-core GitHub-hosted runner → `NumCPU`=2 → 1 worker → fine today.
- Container with `--cpus=2` on a 64-core host, or a k8s self-hosted runner →
  `NumCPU` reports **the host's** 64 → 63 instances → OOM despite the quota.

**Fix: `workers := runtime.GOMAXPROCS(0) - 1`.** We are on **go 1.26.3**, and
Go 1.25+ made `GOMAXPROCS` container-aware, so this single change makes the
worker count respect cgroup limits automatically AND honours an explicit
`GOMAXPROCS` for anyone who wants to cap us. It is strictly more correct than
`NumCPU` on every platform and identical on a bare VM.

Gate it the same way as everything else: byte-identical output (worker count
must not affect the graph — if it does, that is an ADR 5-class bug), and a
measurement at `GOMAXPROCS=2` showing the footprint actually falls near 1
worker's worth.

### D1 — bound instances globally, not per module

The instance pool must be a process-wide resource with a cap, not something
each `ExtractPaths` call allocates independently. Options to measure:
per-module worker budget = `max(1, (NumCPU-1) / modules_in_flight)`; a shared
pool of N instances that modules borrow from; or serializing module gathers
when projected footprint exceeds a threshold.

Note the symbol-table cache (`45087a6`) already proved instances are shareable
across modules — the same reasoning extends to parse instances.

### D2 — question the 64 MB floor

ADR 8 rejected `WithMemoryCapacityFromMax` because eager allocation needs
`WithMemoryLimitPages` and the measured peak was 282 MB x 17 workers. That
analysis assumed one module. It should be re-run against the real constraint
(total footprint across modules), and paired with a smaller initial memory —
64 MB per instance is a lot when 17 of them are idle-sized for one 1.8 MB file.

## Ceiling 2 — `query` reads the whole node file; `card` does not

Measured on a QUIET machine (load 1.90), linux v6.9:

| verb | time | why |
|---|---|---|
| `card <symbol>` | **22 ms** | index-backed (`5a46dd6`) |
| `query "<terms>"` | **3,516 ms** | reads + parses **855 MB** of `nodes.ndjson` |

**`query` did NOT regress.** The recorded baseline is 4,039 ms and HEAD is
3,516 ms — **13% faster**. An earlier report of 6,162 ms was taken at load
8.7–12.2 and is noise; the number is corrected here so it is not repeated.

But it is our weakest axis in absolute terms, and codegraph's recorded 536 ms
is ~6.6x faster. The cause is architectural, not incidental: **the whole node
file is deserialized before the question is even known.** `card` already solved
this shape with a lookup index and went 1.8 s → 22 ms.

### D3 — give `query` the treatment `card` already has

Lexical scoring needs term → node postings, which is an index, not a scan.
Constraints: it must stay deterministic and git-diffable, must be rebuilt as
part of the gather (never lazily on first query — that would make the first
query pay for everyone), and must degrade to the current scan if absent so old
stores keep working.

Measure before building: what fraction of the 3,516 ms is read, JSON parse, and
scoring? If parse dominates, a compact index makes it vanish; if scoring
dominates, the index only helps the read.

## Gates

- Peak RSS on reqsume must fall well under 8 GB (the CI-runner constraint) with
  byte-identical output; measure kubernetes and linux too since D1 changes
  worker scheduling.
- `query` latency: report before/after on linux and kubernetes; judged
  scoreboard may not move DOWN (a faster query that ranks worse is a loss).
- Determinism gates (now byte-identity on both tiers) must stay green — D1
  changes concurrency, which is exactly what ADR 5 taught us to distrust.
