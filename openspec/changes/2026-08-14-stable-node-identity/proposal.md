# ADR 5 — stable node identity: a citation must not move between gathers

Status: DRAFT — owner review pending 2026-08-14. No product code until agreed.
Scope: `internal/extract/code` (id minting + sort) and `internal/store` (sort).
Found by the 2026-08-14 regression audit. **Pre-existing — NOT caused by that
day's 30 commits**, and proven so below.

## The defect

Two gathers of the same source with the same binary produce **different node
locations**. Measured on the pinned corpora:

| corpus | nodes whose `location` changed between two identical gathers |
|---|---|
| linux `block/` | **354** — 329 `struct`, 24 `function`, 1 `enum` |
| Newtonsoft.Json | **229** — 225 `method`, 4 `class` |
| ctx-optimize, reqsume | 0 |

Examples, same binary, back-to-back runs:

```
struct   bdev.c::bd_finish_claiming.block_device   L566-L566 -> L569-L569
struct   bdev.c::bd_may_claim.block_device         L472-L472 -> L475-L475
function blk_pm_resume_queue                       L9-L17    -> L25-L28
enum     mq-deadline.c::dd_prio                    L48-L53   -> L109-L…
```

Reproduce:

```sh
cd ~/ctx-golden-corpora/linux/block
CTX_OPTIMIZE_STORE=/tmp/a ctx-optimize add .
CTX_OPTIMIZE_STORE=/tmp/b ctx-optimize add .
diff <(sort /tmp/a/block/graph/nodes.ndjson) <(sort /tmp/b/block/graph/nodes.ndjson)
```

**Not a regression from the boundary/markdown/importresolve work**: the audit
reproduced it by gathering **twice with the pre-session baseline binary**
(`0a2b192`) — 229 drifts on Newtonsoft, 355 on linux. Today's commits changed
neither the id scheme nor the sort.

## Why it matters more than the count suggests

This is the one failure mode the project's whole pitch forbids. `card` and
`query` answer with `file:line` and the instructions tell agents to **cite it
directly and not re-verify in source**. A location that silently moves between
gathers means:

1. **A citation can be wrong.** The reader lands on an unrelated line, and the
   store gave no signal that it was uncertain — no AMBIGUOUS tier, no warning.
   Every other uncertainty in this system is labelled; this one is not.
2. **`verify` can disagree with itself** across two gathers of an unchanged repo.
3. **The store stops being git-diffable** — the doctrine in CLAUDE.md is "plain
   files, sorted output, atomic rename" so a store can be committed and
   reviewed. A re-gather of unchanged source currently yields hundreds of
   spurious line diffs.
4. **Golden `must_nodes` location pins can flake** — they pass today only
   because they pin suffixes, not lines.

## Root cause — two bugs stacked

**(a) Distinct declarations collapse to one id.** The id scheme
(`<file>::<scope>.<name>` / `<file>::<name>`) is not unique for same-name
declarations in one scope: several `struct block_device *bdev` locals in one C
function, C# method overloads, a forward-declared enum and its definition. The
graph then holds ONE node claiming ONE location for what are several distinct
source facts. Final output has **zero duplicate ids** — the collision is real
but silently resolved.

**(b) The tie-break is undefined.** `sort.Slice` is documented as **not
stable**, and the comparators sort on the id alone:

- `internal/extract/code/code.go:1107` — `sort.Slice(b.Nodes, … ID < ID)`
- `internal/store/store.go:256` and `:372` — same shape

For two nodes sharing an id the comparator says "neither is less", so their
relative order is whatever `pdqsort` leaves behind — and that varies with input
order and slice length. Whichever copy survives dedup therefore varies per run.
(b) is what makes (a) *visible*; fixing only (b) makes the wrong answer stable,
which is worse — a confidently repeatable lie.

## D1 — make the ordering total (necessary, not sufficient)

Every sort over nodes/edges gets a **total** comparator: id, then `location`,
then `kind`, then `source`. Two runs then agree regardless of sort algorithm.
`sort.SliceStable` alone is NOT sufficient — it preserves *input* order, which
is itself parallel-worker dependent.

## D2 — stop minting colliding ids (the real fix)

An id must name one source fact. Options, to be decided with measurement:

- **D2a — qualify by line** when a `(file, scope, name)` already exists:
  `bdev.c::bd_may_claim.block_device@472`. Honest and cheap; changes ids for
  colliding decls only (locations stay put for everything else), but it churns
  ids for repos that have collisions and needs a golden review.
- **D2b — drop local variable declarations from the graph.** 329 of linux's
  354 drifts are `struct` nodes that are function-local variables, not type
  definitions. They may be noise we should never have emitted — a `struct`
  node for a local is not a thing anyone queries. Measure the query impact
  before choosing: this is the smallest graph and may also be the best one.
- **D2c — keep the collision but mark it.** Emit the first and stamp
  `metadata.collides_with: N` so the reader knows the location is one of
  several. Cheapest, and consistent with how this project labels every other
  uncertainty rather than hiding it.

Recommendation: measure D2b first (it may delete the problem class outright and
shrink the graph), fall back to D2a, with D1 landing regardless.

## Gates

- The golden net gains a **determinism test**: gather a fixture twice, assert
  `nodes.ndjson`/`edges.ndjson` are byte-identical. This should have existed;
  its absence is why a non-deterministic store shipped.
- Corpus tier: run the same assertion against linux `block/` and Newtonsoft,
  the two corpora that actually exhibit it.
- Node counts may move if D2b is chosen — that is a reviewed floor change with
  a note, not an automatic accept.

## Kill criterion

If D2b's removal of function-local declaration nodes costs measurable ground on
the judged scoreboard (16.5 / 13.0), it is the wrong fix and D2a is taken
instead. Scores may not move down for a cleanup.
