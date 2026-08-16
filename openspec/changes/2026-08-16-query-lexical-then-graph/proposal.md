# ADR 25 — lexical anchors, then the graph: no embeddings

Status: DRAFT — 2026-08-16. Owner: document today's `query` honestly, then
hand the *next* engine to another agent. No product code in this change.
Date: 2026-08-16
Scope: `internal/query` + gather-time derived index. Site copy that cites
this ADR may land on `docs/see-the-graph` (`/concepts`, `/how-it-works`).

Supersedes **ADR 12 D3** (query postings, still DRAFT) by naming the full
stack and the quality step D3 did not: neighbours become *candidates*, not
just display. Does **not** reopen ADR 5 (determinism), ADR 14 D1 (exact
tier + `port` in `callableKind`, shipped), or the card index (ADR 12
in-scope, shipped).

## The lock

**No vector index. No embeddings. No reranker model. No LLM inside
retrieval.** The host agent sits outside. The engine stays deterministic
IR + the graph we already have. That is the product. Recent work that
fits this lock (LARGER 2605.16352, q-IDF 2605.18561, Zoekt / GitHub
ngram search, Snap-on-Zoekt) is *evidence the direction is live*, not a
licence to import their stacks.

## What `query` actually does today (2026-08-16)

Read `internal/query/query.go`. Not the marketing sentence.

```text
question
  → Tokenize (camelCase / acronym / snake) + drop question stopwords
  → score EVERY node
        tier 0  exact id | label | metadata.identifier
        tier 1  exact token · IDF
                prefix (0.7× IDF, rarest match, never map-first)
                trigram Dice≥0.5 (0.4× IDF, rarest match)
        intent  stubs ×0.25 · tests ×0.5 · docs ×0.5
                (off if the query said test / import / doc|adr|spec)
        dotted non-callable labels ×0.2
  → sort (tier, then score, then id)
  → attach 1-hop neighbours, cap 12
  → stop at ~budget tokens (chars/4, default 2000) or 20 hits
```

IDF is `0.1 + ln(N / (1+df))` with `N = len(nodes)+1`. Named hits are a
**sort key**, not `+1000`. DF is rebuilt **every query** by tokenizing
every label+source.

**Correction the site must not repeat:** ranking sees
`nodes[i].Label + " " + nodes[i].Source` only. Signature and doc comment
are on the *hit* (`card` / render), they are **not** scored fields.

**Two engines already.** `card` is index-backed (`labels.idx` / `ids.idx`
/ edge idxs) — kernel `card` <20ms, fail-safe. `query` still
deserializes the whole node file then scores O(N). Quiet-machine linux
v6.9: **3,516 ms**, 855 MB parsed. CodeGraph's recorded query is 536 ms.
ADR 12 said this out loud: *a label index does not solve free-text
query.*

Neighbours are **displayed**. They do not re-enter the candidate set.
A symbol that does not contain the words cannot win, even if it is the
EXTRACTED callee of the thing that did.

## Decision

Evolve `query` in this order. Each step is its own shippable slice with
the gates below. Do not skip to 4 before 1 is green.

1. **Inverted token postings + precomputed DF**, built at gather, plain
   sorted text beside `graph/index/`, fail-safe (header size+mtime →
   fall back to today's scan). Candidate generation becomes postings
   intersection/union. **Ranking formula stays logically equivalent** —
   this slice is an access path, not a new scorer. Absorbs ADR 12 D3.
2. **Prefix lexicon** (sorted vocab, binary search `refund*`) and
   **trigram → token postings** so prefix/fuzzy do not scan the vocab.
3. **Field-aware scoring** — label / path / signature / doc as separate
   fields with explicit weights. Today's "doc is on the card, not in
   the score" is the defect this slice names. Weights are measured,
   not guessed; judged floors may only move UP.
4. **LARGER-style structural expansion.** Lexical winners are *anchors*.
   1-hop (then bounded 2-hop) neighbours become candidates scored
   `seed × edgeConfidence × relationWeight × hopDecay`. EXTRACTED call
   1.00, resolved import 0.90, test 0.75, doc 0.60, INFERRED 0.50,
   **AMBIGUOUS excluded**. A symbol with no lexical overlap can now
   surface because the graph says it is the implementation. Neighbours
   stop being decoration.
5. **Optional later, own spikes, not this ADR's first cut:** Leiden /
   community prior (there is already `2026-07-14-community-detection`);
   Personalized PageRank; Datalog-shaped predicates (`calls`, `imports`,
   `reads_env`, `reachable`) where the *agent* writes the predicate and
   the engine still has no model; **q-IDF only as a bench** — the paper
   itself says identifier-aware tokenization eats most of the gain, and
   we already split camelCase.

## What this ADR is not

- Not embeddings, not a hybrid "and then we add a vector lane." Snap's
  line (Zoekt first, embeddings additive) is noted; we do not add the
  additive lane here.
- Not an LLM that writes Datalog (LogicLoc). Predicates, if they land,
  are a CLI/JSON surface the host agent fills.
- Not "fastest query in the field" as a headline. Postings should put
  us in the same class as an indexed lexical engine; say the number
  after the bench, pin-verified.
- Not a change to `card`'s index format except that postings live
  next to it and share the fail-safe header discipline.
- Not a silent ranking rewrite bundled with the index. Slice 1 must
  prove equivalence; slices 3–4 are allowed to change rank and must
  prove the scoreboard.

## Docs consequence

`/concepts` and `/how-it-works` describe **today's** path (including
label+source only, O(N) scan, neighbours displayed not re-ranked). They
must not claim doc comments are scored or that `query` uses `labels.idx`.
The next engine is this ADR, not a site promise.

## Gates

- `task ci` + `task golden`. Judged floors may not move DOWN.
- Slice 1: same hits, same order as scan on a pinned corpus (linux
  block + this repo), byte-identical `query --json` except perhaps
  display-score lift that already exists. Fail-safe: delete the
  postings file, answers still match.
- Slice 1 latency: report before/after on linux and kubernetes, quiet
  machine, best-of-3, `CTX_OPTIMIZE_STORE` temp. No "fastest" until
  `versions.json` exists (arena is still unpinned — Agents.md).

  **BEFORE, measured 2026-08-16** so slice 1 has something to beat:

  ```
  linux    2,848,839 nodes   3.93s best of 5   (4.04 4.00 3.99 3.93 4.19)
  reqsume     19,808 nodes   0.04s
  ```

  Same binary, same question ("buffer allocation retry"). 144x the nodes,
  ~100x the time — the O(N) claim is not theoretical. NOT pin-verified:
  load average was 5.65 on a 3-day-up laptop, so treat it as the shape of
  the curve, not a headline. Re-take on a quiet box before publishing.
- Slices 3–4: scoreboard and the mux 67%/15-call run may only move UP.
  A faster query that ranks worse is a loss.
- Gather cost of building postings reported; atomic rename; git-diffable
  sorted output; no secrets in the index.

## SPIKE 2026-08-16 — postings addresses 20% of the cost. The edges are 64%.

`scripts/spikes/queryphases`, linux (2,848,839 nodes / 5,539,232 edges),
phases mirrored from query.go so the boundaries are visible:

```
PHASE read nodes    0.589s   16%
PHASE tokenize+df   0.731s   20%   <- what postings moves to gather
PHASE read edges    1.073s   29%   <- postings does NOT touch this
PHASE adjacency     1.293s   35%   <- nor this
PHASE score        ~0.110s    3%
SUBTOTAL            3.687s          (real verb: 3.93s, so ~94% accounted)
```

**`query` reads all 5.5M edges and builds a whole-graph adjacency map — 11M
appends — on every call, to attach at most 12 neighbours to at most 20 hits.**
That is 2.37s, 64% of the verb, and slice 1 as written does not touch it.

The index that fixes it is already on disk. `graph/index/edges-by-source.idx`
(98 MB) and `edges-by-target.idx` (231 MB) are built at gather and used by
`card`; `query.go` never opens them. Measured on the same 20 nodes and the
same 37 neighbours:

```
INDEXED  40 lookups via EdgesFrom/EdgesTo   39.5ms
SCAN     full read + adjacency map           2.215s
                                             56x, identical results
```

**So the slice order should change.** A neighbour-attachment slice is bigger,
cheaper and lower-risk than postings: no new index, no new file format, no
ranking change at all — the hits and their order are untouched, only how the
neighbours are fetched. It should be slice 0.

Two more results from the same run:

- **Union, not intersection** — the open question below, answered. Candidate
  sets for real agent questions: `split bio segments` 6,974 (0.24%),
  `buffer allocation retry` 24,262 (0.85%), `elevator hash add request`
  70,054 (2.46%). Selectivity is excellent, so postings will work. But the
  INTERSECTION is **0** for every multi-word query tested — no single node
  carries all of a 3-4 word question's tokens. An AND default would return
  nothing.
- **Postings would cost ~88.7 MB on linux** (23.2M (node,token) pairs as
  uint32; vocabulary 68,798; df median 7, p90 205, max 1,725,630) on top of an
  index directory that is already 631 MB. Worth stating before building it.

### The complexity answer: not O(1), not O(N) — O(candidates)

"Do we not have an index now, and does this become O(1)?" Measured, per phase,
on the linux store:

```
phase                    today      indexed          mechanism
1 read nodes             607ms      26.5ms  (23x)    NodeByID / ids.idx      EXISTS
2 tokenize + DF          731ms      ~0               precomputed at gather   NEW (postings)
3 read edges + adjacency 2,370ms    39.5ms  (56x)    EdgesFrom/EdgesTo       EXISTS
4 score                  110ms      ∝ candidates     postings                NEW
```

**It cannot be O(1).** Ranking has to see every node that contains a query
token; that set is data-dependent, so constant time is not on the table for
any honest ranker.

**It does not have to stay O(N).** With postings the shape becomes

```
O( Σ |postings(t)| for t in query  +  hits · log N )
```

and the candidate sets are small on real questions — measured 0.24% to 2.46%
of the corpus. Sublinear in N, linear in what actually matched.

**Worst case is still O(N)**, and that is not hypothetical: linux has a token
with df 1,725,630 — 61% of every node. A query made only of such tokens
selects most of the corpus however it is indexed. That is stress-spec I5, and
the answer there is a stated cap, not a faster scan.

**Two of the four phases need no new index at all.** `NodeByID`,
`EdgesFrom` and `EdgesTo` are shipped, tested, and used by `card`; `query`
simply never calls them. Phases 1 and 3 are 2.98s of the ~4.0s — **74% of the
verb — and the fix is calling code that already exists.**

Projected floor if all four land: roughly 100–300ms, from 4.0s. That is a
PROJECTION from per-phase measurements, not a measured end-to-end number, and
it must be re-measured after slice 0 before it is repeated anywhere.

### Slice 4 headroom — directional, NOT an accuracy number

Same spike, `-run TestStructuralHeadroom`, 14 judged `query` questions from
`linux-block.json` (the 6 card/path/affected questions are skipped — running
`query` for those and scoring the miss measures the spike's mistake, which the
first run did):

```
lexical top-k correct           3
MISSED but within 1 hop         4     <- what expansion could reach
missed, not within 1 hop        7
```

**This is not comparable to the judged scoreboard and must not be quoted as
accuracy.** The floor is 16.5/20; this run scores far under it because the
store is the WHOLE kernel, while the scoreboard's corpus tier gathers the
pinned `block/` subsystem. Asking "elevator hash add request" across 2.85M
nodes is a different question from asking it across `block/`.

What it does say, with that caveat: **of the questions lexical missed, 4 of 11
had the answer one hop away** — blk-throttle.c, sed-opal.c, blk-flush.c,
ll_back_merge_fn. That is real headroom for slice 4 and it is not nothing. Re-
run against the pinned corpus before any number goes in a commit message.

## SLICE 0 SHIPPED 2026-08-16 — and where the rest of the second is

`query` no longer builds a whole-graph adjacency map. Measured on linux, same
question, same machine: **4.11s → 2.73s**, same top hit, judged floors
unmoved (linux-block 16.5/20, newtonsoft 13.0/20 — both exactly at floor,
which is what an access path must do).

Short of the 2.37s the phase profile predicted, and the reason is worth
recording rather than rounding away: **only the adjacency BUILD is gone. The
caller still reads all 5.5M edges (1.07s).** It reads them because
`narrowQuery` builds a discloser from the PRE-narrow streams — that is
deliberate, it is the only thing that can say what the store holds when a
filtered query comes back empty.

So the remaining 1.07s is not a mechanical edit. It needs the discloser to
answer lazily (what does it actually need, and can it read that on demand?),
and that is its own slice with its own decision. **Do not "optimise" it by
dropping the discloser** — an empty filtered result that cannot explain itself
is a worse product than a slower one.

Two correctness constraints that landed with the slice and must not be
loosened:

- **The indexed lane is used ONLY when nothing narrowed the graph.** A
  `--where`/`--kind` query must see neighbours from the NARROWED graph; the
  index knows only the whole one. Serving it after a filter attaches edges the
  caller explicitly excluded — a wrong answer, not a slow one.
- **The index is probed once, up front.** A per-hit decision can answer half
  from each lane.

And one honest gap: the file-order sort in `EdgesTouchingOrdered` is **not
red-proven**. A mutation removing it survives both the store fixture and the
real-store equivalence test. It is cheap and obviously correct; it is not
demonstrated necessary, and a future reader should know that before trusting
the comment above it.

## An honesty constraint slice 4 needs, and does not yet state

Slice 4 lets a node become a RESULT because the graph vouches for it, with
no lexical overlap at all. That is the point — and it is also the first
time `query` would return something it INFERRED rather than something it
matched.

Everything else this store emits carries its evidence class: edges are
EXTRACTED / INFERRED / AMBIGUOUS, ports name their rule, `card` cites
file:line. A structurally-promoted hit must say so in the output — anchor
vs expansion, and which relation vouched for it — or `query` starts
asserting relevance in the same voice it uses for parsed fact. The
confidence weights in slice 4 decide what surfaces; they do not tell the
reader that anything was inferred.

Cheap to hold: one field on `Hit`, printed in both text and `--json`.
Expensive to retrofit once agents have learned to trust the output shape.

## Open

- Union vs intersection default when the query has several tokens
  (recall vs precision on 2–4 word agent questions).
- Whether signature/doc field weights land in slice 3 or wait for a
  measured sweep (same discipline as `docDemote`).
- Hop budget for expansion (1 vs 2) and whether directory/community
  prior is in slice 4 or a later ADR.
