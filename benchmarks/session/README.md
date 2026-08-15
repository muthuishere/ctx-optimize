# Session benchmark — what an agent actually costs over an hour

The one-pass runners (`../bench.py`, `../bench_multi.py`) measure **time to
build an index**. That is the metric ripgrep, Zoekt and GNU Global are built to
win, and measuring only it flatters grep-class tools while charging graph-class
tools full price for work that amortizes away in practice.

Real agent work is **multi-pass**: build once, ask a dozen questions, edit a few
files, re-index, ask more, repeat for hours. Under that model the cold build is
a rounding error and two things dominate — **incremental update cost**, paid
after every edit, and **answer quality per query**, paid on every question.

This harness models a session, not an index build.

```
PHASE 0  cold build          wall, index bytes, peak RSS
PHASE 1  queries (cold)      latency + graded answers
PHASE 2  scripted edit       deterministic, reverted afterwards
PHASE 3  incremental update  wall   <- the metric that separates tools
PHASE 4  queries (warm)      latency + graded answers + STALENESS probes
         rounds 2..R repeat 2-4; the table reports cumulative session cost
```

## Run it

```sh
python3 benchmarks/session/session.py \
    --session benchmarks/session/sessions/newtonsoft.json \
    --rounds 2 --json results-newtonsoft.json
```

Tools come from **`../suite/tools.json`** — the same committed field manifest the
one-pass runner uses. There is deliberately no second manifest here: session
support is expressed as extra fields (`session_build`, `incremental`,
`session_query`, `query_by_class`, `query_field`, `answers_classes`) on the
existing entries. A tool with no `session_query` is listed but not run, and the
manifest states why.

## The staleness probe — the point of the whole thing

A tool that "re-indexes" in 0 ms by doing nothing is not fast, it is wrong. A
build-time benchmark scores it as the winner. So each round renames a symbol and
asks two questions in opposite directions:

| probe | expectation | catches |
|---|---|---|
| `expect_absent` | the OLD name must be gone from answers | an index still serving dead facts |
| `expect_any` | the NEW name must be findable | an index that never updated |

Both are exact matches on a symbol that provably did or did not exist before the
edit, so grading needs no LLM and no judgement.

## Fairness rules

A benchmark you design and win is worth nothing. These are the rules that make
the table survive a hostile reading:

- **Questions carry a class.** `locate` ("where is X") is answerable by a line
  matcher, and ripgrep wins it below. `relate` ("who calls X", "what breaks if I
  change X") needs a call graph. A single blended score would hide both facts.
- **A tool that cannot answer a class is `n/a`, never `0`.** Scoring a trigram
  indexer 0/10 on "who calls this" is a rigged test, not a finding.
- **Search tools get `q_literal`** — the exact symbol name — instead of the
  natural-language terms. That is deliberately their *best* case and our hardest.
- **Speed rows and quality rows are separate**, so tools that can share one and
  not the other still appear honestly.
- **The question set keeps a known loss.** `L01` scores 0.0 in our own judged
  tier (test files outrank the source method) and is kept in. A set with no
  losses in it is a sales deck.
- **Corpora are pinned, edits are scripted**, both recorded in the results JSON.
  The rename op verifies its own occurrence count and *aborts* if the corpus has
  drifted from the pin, rather than running a silently different edit.
- **A "warm" run that is really a full rebuild is flagged.** `AUDIT.md` caught
  exactly that in the 2026-07-24 run (GitNexus, 2 of 4 corpora); any incremental
  ≥90% of the cold build is marked `suspected_full_rebuild` in the JSON.
- **A broken invocation is never graded as a wrong answer.** `AUDIT.md` also
  caught a broken ast-grep pattern being timed as a real query; here a non-zero
  exit (other than ripgrep's "no matches") is reported as a harness error.

## Measured run — Newtonsoft.Json 13.0.3 (0a2e291), 2 rounds

```
SPEED
tool              cold build     index   peak RSS   incremental  session total
ctx-optimize           0.60s      14MB        3GB         0.56s          2.42s
ripgrep            indexless        0B          —         0.00s          0.32s

QUALITY  (n/a = cannot answer this class, excluded from the score)
tool              locate (cold)  locate (warm)  relate (cold)  relate (warm)   staleness
ctx-optimize                4/5           8/10            2/2            3/4         2/2
ripgrep                     5/5          10/10            n/a            n/a         2/2
```

**Read it honestly.** ripgrep is ~7× cheaper over the session, never stale, and
beats us on `locate` 5/5 to 4/5 — our miss is the known test-noise ranking
defect. What it cannot do at all is `relate`: no line matcher answers "what
breaks if I change `ReadStringIntoBuffer`". That is the whole trade, and this
table states both halves of it in the same font.

On a corpus this small the multi-pass thesis is *not* yet demonstrated — 0.56s
of incremental against a 0.60s build means there is nothing to amortize. The
thesis needs a corpus where the cold build is expensive and the incremental is
not; that run is the next step, not a claim yet.

## What this harness cannot fairly measure yet

1. **The thesis corpus is missing.** Newtonsoft validates the *shape*. The
   argument only bites on linux / kubernetes, where a cold build is tens of
   seconds. Until those run, "multi-pass changes the ranking" is a hypothesis.
2. **Only two tools are adapted.** graphify, codegraph and gitnexus are pinned
   in `../suite/tools.json` and built in `~/ctx-bench-arena/tools/`, but their
   `incremental` commands are not yet expressed. gtags and Zoekt belong in the
   speed rows and are absent entirely — which is exactly the gap
   `docs/CRITIQUE.md` item 6 is about.
3. **`relate` ground truth is hand-authored**, so it carries our idea of a good
   answer. Loc-Bench is the external yardstick that would fix this.
4. **Query latency is wall-clock including process start.** For a 30 ms answer
   that is mostly exec overhead; it flatters long-running servers and punishes
   CLIs. Fine for CLI-vs-CLI, wrong the moment a daemon enters the table.
5. **Peak RSS is the whole process tree's high-water mark** via `/usr/bin/time`,
   not a sampled curve, so a brief spike and sustained use look identical.
6. **One known artifact:** `R01` targets a symbol that round 1 renames, so it
   legitimately fails in that warm round (3/4 rather than 4/4). That is the
   graph correctly refusing to report a symbol that no longer exists, scored as
   a miss. The fix is to point standing questions at symbols the edits do not
   touch; until then the number is explained rather than hidden.
7. **No cross-machine comparability.** Everything here is one machine; the
   numbers are ratios between tools on the same box, never absolutes.
