# ADR 6 — the boundaries lane costs a second walk, 12–24× its budget

Status: SUPERSEDED 2026-08-14 by ADR 7 (`2026-08-14-boundaries-on-the-ast`).
The regression measurements below stand and are the evidence ADR 7 is built on.
**Both fixes proposed here are wrong, and ADR 7's spike proved it with
numbers:** the second READ is only 6-10% of the lane's cost (k8s 330ms of
5.6s; ts 1.36s of 13.8s), so D1 walk fusion could never have reached the ≤+5%
budget; and D2's regex alternation optimises the right 90% while keeping every
accuracy defect the owner ruled out ("never regex kind of"). Kept unedited as
the record of a diagnosis that was right about the cost and wrong about the
cure.
Scope: `internal/boundaries` (how rules are applied), possibly
`internal/extract/code` (walk fusion). No change to the rule schema, the port
model, or any emitted fact — this is purely HOW the same output is produced.

Found by the 2026-08-14 regression audit. **This one IS ours.**

## The regression

ADR 1 budgeted **≤ +5%** on total gather. Measured old (`0a2b192`) vs HEAD,
median of 3 runs each:

| corpus | files | before | after | delta |
|---|---|---|---|---|
| linux `block/` | small | 0.48s | 0.52s | +9.6% |
| Newtonsoft.Json | 1,183 | 0.81s | 1.21s | **+50%** |
| reqsume (multi-module) | 4,883 | 0.96s | 1.53s | **+60%** |
| java-spring | 10,290 | 6.67s | 10.71s | **+61%** |
| go-kubernetes | 24,471 | 15.00s | 23.93s | **+60%** |
| ts-typescript | 71,754 | 11.50s | 25.26s | **+120%** |

It does not amortize — it gets **worse** with file density, which is the wrong
direction for a tool whose pitch is "seconds on a big repo".

⚠️ **Error bars on these wall-clock numbers.** They are medians of 3, but they
were taken on a machine running several heavy agents concurrently. A later
profiling pass re-ran the same kubernetes A/B twice and got **+16.8% then
+4.9%** — the base swung 10% and the new build 19% between runs. **Treat
anything under ~20% measured by CLI wall-clock that day as noise.**

The DIRECTION is not in doubt, because two measurements immune to machine load
agree with it: (a) the bisect below is monotonic across five builds, and (b)
the ADR 7 spike measured the regex/read split **inside a single process** —
5.29s of regex against 330ms of walk+read on kubernetes, a ratio no amount of
CPU contention can invert. The magnitude has error bars; the diagnosis does
not.

Bisected on go-kubernetes, and the attribution is unambiguous:

| build | time | attribution |
|---|---|---|
| `0a2b192` baseline | 16.00s | — |
| `f46b92d` search + store fix | 16.11s | +0.7% (noise) |
| `21ad21e` **boundaries lane** | 19.45s | **+20.9%** |
| `9f80fc4` importresolve (D7) | 20.27s | +5.1% |
| `e4c26fe` **route/service rules** | 24.82s | **+28%** |

**Markdown/goldmark is exonerated** — on a markdown-only corpus (549 files,
4.7 MB) the new producer is *faster*, 0.30s → 0.23s, while removing the 66
phantom nodes. ADR 4's <50 ms budget holds. D7 is inside budget at +5.1%.

The cost is the boundaries producer: **a second full-tree walk that opens and
scans every file again, running 14 rules per file**, and the route/service
rules roughly doubled the rule count.

## We violated our own doctrine, in writing

ADR 2's D4 hard rules already say it:

> a rule that cannot ride the engine's walk is **declared, not smuggled in as
> a second pass**

The boundaries lane *is* the second pass. The rule was written about
skill-authored rules and then broken by the built-in implementation.

## Mitigants (real, but not a defence)

- **Incremental gathers are untouched.** Second run on py-django: 4.45s →
  **0.14s** (32×), byte-identical graph. The regression is paid only on a
  full or `--force` gather.
- Correctness is unaffected — the audit confirmed every node/edge delta is
  ADR-explained, and determinism holds.

That still means every FIRST gather — the one a new user experiences, and the
one every CI job runs — got 60% slower on real repos.

## D1 — one walk (preferred)

Fuse boundary rule evaluation into the existing extractor walk: the file is
already open and its bytes already in memory for tree-sitter. Rules then cost
regex time only, not a second stat + read + decompress cycle. This is what the
doctrine assumed all along.

Risk to weigh: the code walk is parallel per-worker and boundaries currently
merges serially with deterministic sorting. Fusion must not disturb output
order — the audit's byte-identical determinism check becomes the gate.

## D2 — one regex pass per file, not fourteen

Independently of D1: rules sharing an extension set should compile to **one
alternation** with named/numbered submatch groups, so a file is scanned once
rather than 14 times. Go's RE2 handles alternation in a single linear pass;
this is exactly the shape it is good at. Expected to recover most of the
`e4c26fe` +28% specifically, since that commit's cost was rule COUNT.

## D3 — measure before choosing

Spike both on go-kubernetes and ts-typescript (the two worst cases) and pick by
measurement, not by which is prettier. If D2 alone lands the budget, D1's
concurrency risk need not be taken.

## Budget and gates

- Restore **≤ +5%** total gather vs `0a2b192` on all six corpora above. If that
  proves impossible, the honest move is to amend ADR 1's budget with the
  measured number and the reason — not to quietly keep +60%.
- The golden net gains a **perf ceiling** for the corpus tier: it currently
  pins node counts and query latency but never gather wall-time, which is why
  a 60% regression shipped green.
- Byte-identical determinism before/after (the audit's method) is the
  correctness gate for any fusion work.

## Kill criterion

If neither D1 nor D2 gets below +15%, boundaries should become **opt-out on
full gathers of very large trees** (a flag or a size threshold), so the default
first-gather experience stays fast — with the cost stated in the docs rather
than hidden.
