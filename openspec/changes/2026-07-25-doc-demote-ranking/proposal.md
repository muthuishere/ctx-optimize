# ADR — prose must not outrank code when the question is about code

Status: **IMPLEMENTED** — 2026-07-25.

## The problem, measured

Dogfooded `query` against ctx-optimize's own store (3,963 nodes, gathered in
0.71s) with 10 real "where is X implemented" questions. Lexical scoring answered
with the ADR *about* X instead of X:

| | pre-fix |
|---|---|
| recall@1 (right symbol is the top hit) | **5 / 10** |
| a `.md` node holds #1 | **4 / 10** |
| top-3 slots taken by `section` nodes | **15 / 30** |

Concretely: "match declaration by head symbol" returned the ADR three times and
never `headDecl`; "prune stale nodes on add" and "budget query hits ranking"
both returned `README.md` at #1; "resolve call to unique name" put an openspec
proposal above the function.

The cause is not mysterious. Doc nodes are **40% of this graph** (1,315
`section` + 278 `document` of 3,963) because `openspec/` is large, and prose
repeats a question's words far more often than code does. IDF-weighted token
overlap therefore prefers the essay to the implementation. Any repo with real
design docs has this shape.

## Decision

Scale `section`/`document` scores by **0.5** in `intentAdjust` when the question
is not about prose.

This is not a new mechanism — `intentAdjust` already demotes `module://` nodes
(0.25×) and test sources (0.5×) unless the question asks for them. Prose is the
same problem, so it gets the same treatment and the same escape hatch:
`docIntent` (doc, docs, readme, changelog, adr, spec, proposal, design, guide,
wiki, rationale, decision, openspec) turns the demote **off** entirely.

A demote, not a filter. A doc node still wins when it genuinely is the best
answer — which is why `verify`, `explain` and the wiki are untouched.

## Why 0.5

Swept, not chosen (`TestDocDemoteChosenByMeasurement` keeps the sweep runnable):

| docDemote | recall@1 | #1 is prose | recall@3 | doc-intent survival |
|---|---|---|---|---|
| 1.00 (pre-fix) | 5/10 | 4/10 | 8/10 | 3/3 |
| 0.75 | — | — | 8/10 | 3/3 |
| 0.60 | 7/10 | 1/10 | 8/10 | 3/3 |
| **0.50 (shipped)** | **7/10** | **0/10** | **9/10** | **3/3** |
| 0.35 | 7/10 | 0/10 | 9/10 | 3/3 |
| 0.25 | 7/10 | 0/10 | 9/10 | 3/3 |

0.5 is the **mildest** value that reaches every maximum — recall@1 saturates,
prose stops holding #1 entirely, recall@3 gains one. Going further buys nothing
and only pushes docs down harder. Least intervention that gets the result.

Note what the numbers say honestly: recall@**3** barely moves (8→9). The failure
was almost entirely at rank 1, which is exactly the position an agent acts on.

## No regression

- Judged scoreboards **unchanged**: linux-block 16.5/20 (floor 16.5), newtonsoft
  12.5/20 (floor 12.5). Those corpora are code-heavy, so the demote has little
  to bite on — the point is that it costs nothing there.
- Every golden snapshot passes, including `queryTop` rankings on the fixtures.
- `task ci` green.
- `TestDocDemoteBeatsBaseline` asserts the shipped value beats no-demote on
  code-intent AND loses no doc-intent question, so a future tweak that helps one
  by breaking the other fails.
- `TestDemoteScopedToProse` pins that no code kind is ever demoted.

## Not claimed

- The question set is 10 questions on ONE repo, written by the same person who
  wrote the fix. It is a real measurement, not an unbiased benchmark.
- The corpus fixture is a gathered store, too large to commit, so
  `TestDocDemoteChosenByMeasurement` and `TestDocDemoteBeatsBaseline` **skip**
  unless `CTX_OPTIMIZE_DOCDEMOTE_STORE` points at one — the same opt-in shape as
  the golden corpus tier. The hermetic tests (`TestDocIntentDisablesDemote`,
  `TestDemoteScopedToProse`) always run.
- 3 of 10 code-intent questions still miss at rank 1. This narrows the gap; it
  does not close it. The judged tier's own gaps (L17–L20, N17/N19/N20) are
  untouched by this change and remain the standing quality target.

---

## Follow-up: question grammar was scoring as signal

The doc demote left 3 of 10 code-intent questions wrong at rank 1. Diagnosing
each one — rather than tuning further — found that only ONE was a ranker defect:

| question | #1 returned | verdict |
|---|---|---|
| "resolve call to unique name" | `wiki.go::uniqueName` | defensible — matches *both* "unique" (df=1, idf 7.59) and "name"; the expected answer was arguable |
| "compose service depends on" | `compose.yaml#depends-on` | defensible — a config key literally named `depends-on` in a compose file answers that question |
| "prune stale nodes on add" | `install.go::OnPath` | **defect** |

`OnPath` won on the word **"on"**. IDF is computed over identifier tokens, where
`on` has df=49 of 3,963 → **idf 4.37 — higher than `name` at 4.41**. So English
question grammar was acting as a rare, high-signal discriminator. Same for `to`
(df=48, idf 4.39).

**Decision: drop a small stopword set from the QUERY, never from node tokens.**
Kept deliberately narrow — only words that cannot be a search term on their own.
`get`, `set`, `new`, `run`, `add`, `list`, `call`, `name`, `path`, `file` are
explicitly NOT stopwords (they are real identifier prefixes; dropping them would
break `query "get user"`), pinned by `TestStopwordsKeepIdentifierWords`. An
all-stopword question still searches literally rather than returning nothing
(`TestStopwordsNeverEmptyTheQuery`).

### Result — the independent corpus is the evidence

| | before | after |
|---|---|---|
| **newtonsoft judged** | 12.5 / 20 | **13.0 / 20** (floor ratcheted) |
| linux-block judged | 16.5 / 20 | 16.5 / 20 |
| own-repo recall@1 | 7/10 | 7/10 |

N16 — "Where do JSON converters get chosen **for a** type?" — went **0.5 → 1.0**
once `do`/`for`/`a` stopped diluting the real terms. That is a corpus nobody
tuned against, which is worth more than the hand-written set.

Own-repo recall@1 did **not** move: the remaining miss on "prune stale nodes on
add" now returns `anyStale` (`autosync.go`) instead of `OnPath`
(`skills/install.go`) — topically relevant instead of grammatically lucky, but
still not the expected `store.Replace`, so the binary metric is unchanged. The
answer improved; the score did not. Both are reported rather than picking the
flattering one.

### The remaining ceiling is recall, not ranking

`store.Replace` cannot win "prune stale nodes" by any amount of reranking: only
`Label + Source` are tokenized, and the word "prune" appears in its **doc
comment**, which is not indexed. That is a recall ceiling. Closing it means
indexing doc/signature text — bigger store, churn in every golden snapshot, and
its own precision risks. NOT attempted here; recorded as the real next lever.
