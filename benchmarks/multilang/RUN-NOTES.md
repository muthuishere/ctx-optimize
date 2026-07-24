# Multi-language cross-tool benchmark — RUN NOTES (2026-07-24)

**Every number below is [MEASURED]** by `bench_multilang_big.py` +
`warm_resync_fix.py` (this dir), run on the stamped machine, on freshly
cloned pinned repos in `~/ctx-bench-arena/multilang/` (arena, not the repo
tree). Raw per-cell logs are in `~/ctx-bench-arena/multilang/logs-big/` and
`~/ctx-bench-arena/multilang/logs/` (not committed — too big; paths
referenced here for reproduction). Quality-capture raw dumps are in
`~/ctx-bench-arena/multilang/quality-big/<corpus>/<tool>__<symbol>.txt`.

## Machine + versions

```
Apple M5 Pro, 18 cores, 48 GB
ctx-optimize v0.8.0-4-g3fa2649 (3fa2649, 2026-07-24T05:53:10Z)  -- built fresh from ctx-optimize-bench-wt HEAD, NOT the stale v0.7.0 release
codegraph 1.5.0
gitnexus 1.6.9
graphify 0.9.12
ast-grep 0.45.0
```

## Scope change (owner-directed, mid-run)

The task started against `benchmarks/multilang/corpus-manifest.json`'s
**small, bounded-subdir** manifest (c-linux/block ~101 files, py-flask/src,
py-django/django, go-gin, java-gson, csharp-newtonsoft, ts-hono). While that
pass was mid-flight (only `c-linux` had completed), the coordinator
redirected to a **BIG real-flagship-repo** manifest to test whether
ctx-optimize's edge holds AT SCALE — the actual point of this benchmark. The
small pass was killed after 1 corpus; its `c-linux` result survives as
`corpora_small_control_partial` in `results-multilang.json`, useful only as a
sanity cross-check, NOT the primary result. **All headline numbers below are
from the BIG-repo pass.**

## Corpora (BIG pass, pinned refs, real file counts — re-counted this run)

| id | lang | repo@ref | subdir | files (measured) |
|---|---|---|---|---|
| c-postgres | c | postgres/postgres@REL_16_3 | src | 5245 |
| py-django | python | django/django@5.0.6 | django | 3647 |
| go-kubernetes | go | kubernetes/kubernetes@v1.30.2 | pkg | 3125 |
| java-spring | java | spring-projects/spring-framework@v6.1.10 | (root) | 10142 |
| csharp-efcore | csharp | dotnet/efcore@v8.0.6 | src | 2677 |
| ts-typescript | typescript | microsoft/TypeScript@v5.5.3 | src | 723 |

Clone SHAs: c-postgres `05ffe9398b75`, py-django `2719a7f8c161`, go-kubernetes
`39683505b630`, java-spring `5356a1b1ac98`, csharp-efcore `6a2be34d0453`,
ts-typescript `f0e992167440`.

## Methodology (and where it deviates from the small-corpus pass, honestly)

- **Cold gather = a SINGLE run** (not best-of-3), capped at `CELL_TIMEOUT_S =
  600s` per (tool, corpus) cell. Doing 3 cold runs of a multi-minute tool on
  a 10k-file repo would not fit the session; a single real number, capped and
  labeled, beats no number. No cell hit the cap — the slowest single cell was
  gitnexus on java-spring at 234.5s.
- **Warm (0-change) uses each tool's own sync-equivalent verb**, invoked
  TWICE (once untimed to "prime" the store's freshness-head, once timed) —
  see the "ctx-optimize `up` bug I found and fixed" section below for why
  priming is required:
  - ctx-optimize: `up` (not `add` again — that was the small-pass's
    methodology and undersells ctx-optimize's real no-op path)
  - codegraph: `sync`
  - gitnexus: `analyze` (same args — GitNexus has no separate incremental verb)
  - graphify: `update --no-cluster` (same args)
- **A new `resync_1file_s` cell**: commit a 1-line trailing comment to ONE
  real source file per corpus, then time each tool's sync verb, then
  `git reset --hard <pinned sha>` to restore the corpus byte-identical to the
  manifest ref. This is the actual "I changed one file, how fast does the
  index catch up" number — the number a real dev workflow cares about.
- **Query latency**: median of 5, 60s timeout per run, `--budget 2000` where
  applicable. ast-grep pattern **verified working (rc=0, non-empty output)
  on every one of the 6 languages** before being timed — the earlier
  cross-tool run's broken zero-match Python pattern is NOT repeated (see
  Issue 1 in `openspec/changes/2026-07-24-cross-tool-benchmark/evidence/AUDIT.md`).
  Patterns used: c `$RET $NAME($$$) { $$$ }`, python
  `def $NAME($$$):$$$BODY`, go `func $NAME($$$) { $$$ }`, java/csharp
  `public class $NAME {$$$BODY}` (bare `class $NAME {...}` returns 0 matches
  in both — Java/C# top-level classes are `public`), typescript
  `function $NAME($$$) { $$$ }`.
- **Quality capture**: 3 real symbols per corpus (2 for java-spring/csharp-efcore,
  1 for ts-typescript — picked by grepping the actual source, see below),
  verbatim stdout saved for ctx-optimize `card` + `query`, graphify `query`,
  codegraph `explore`+`query`, gitnexus `context`+`query`, a verified ast-grep
  run, and a `grep -rn --exclude-dir={.git,.codegraph,.gitnexus,graphify-out,
  .ctxoptimize,node_modules}` baseline. No judging happened here — that's a
  separate pass; this only guarantees the raw material is honest and complete.

## A real bug I hit and fixed mid-run: `up`'s freshness check is git-HEAD-based

`ctx-optimize up`'s staleness check compares the store's recorded git HEAD to
the corpus's *current* git HEAD — **not** a file-mtime or working-tree scan.
Confirmed by hand: appending an uncommitted comment to a tracked file and
running `up` returned in 0.047s reporting "up to date with git HEAD" — the
edit was invisible until committed. This is itself a reportable behavior
(ctx-optimize will not notice uncommitted working-tree changes via `up`
until they're committed), and it also means: after any exploratory
`up`/commit/`git reset --hard` cycle on a corpus (which I did once on
c-postgres while developing this script), the STORE's remembered head can
end up out of sync with the corpus's actual head, and the next "0-change
warm" measurement silently becomes a **full re-gather** instead of a true
no-op. I hit this for real on c-postgres, caught it (the first attempt logged
4.83s for a "0-change" cell, which is cold-gather-scale, not warm-scale), and
fixed the runner to always fire one **untimed priming `up`** before the timed
one. After the fix every corpus's 0-change `up` is a genuine <0.1s no-op —
see the table below.

## Headline table — cold gather (s) / store size (MB) / query (ms), all 4 gather-capable tools

| corpus (files) | tool | cold_s | store MB | query_ms |
|---|---|---:|---:|---:|
| **c-postgres** (5245) | **ctx-optimize** | **4.53** | **41.9** | 83 |
| | codegraph | 8.74 | 174.9 | **67** |
| | graphify | 26.35 | 158.1 | 1026 |
| | gitnexus | 44.74 | 983.2 | 649 |
| **py-django** (3647) | **ctx-optimize** | **1.39** | **8.7** | **32** |
| | codegraph | 1.81 | 37.1 | 76 |
| | graphify | 9.91 | 32.2 | 463 |
| | gitnexus | 13.27 | 268.5 | 698 |
| **go-kubernetes/pkg** (3125) | **ctx-optimize** | **4.53** | **37.9** | 66 |
| | codegraph | 8.38 | 212.4 | **65** |
| | graphify | 27.10 | 157.8 | 1519 |
| | gitnexus | 44.36 | 856.7 | 618 |
| **java-spring** (10142) | **ctx-optimize** | **10.14** | **135.2** | 221 |
| | codegraph | 19.19 | 686.3 | **71** |
| | graphify | 160.19 | 692.2 | 5767 |
| | gitnexus | 234.46 | 1666.1 | 634 |
| **csharp-efcore** (2677) | **ctx-optimize** | **2.03** | **24.3** | **48** |
| | codegraph | 6.18 | 138.3 | 69 |
| | graphify | 29.02 | 122.6 | 1106 |
| | gitnexus | 19.69 | 498.3 | 648 |
| **ts-typescript** (723) | **ctx-optimize** | **1.77** | **18.5** | **42** |
| | codegraph | 3.60 | 86.5 | 65 |
| | graphify | 21.02 | 30.6 | 771 |
| | gitnexus | 17.78 | 334.8 | 634 |

**Bold** = best in column, per corpus.

## PERF-HOLDS verdict — does ctx-optimize's edge survive at scale, per language?

| language | corpus | cold-gather rank | store-size rank | PERF HOLDS? |
|---|---|---|---|---|
| c | c-postgres (5245 files) | **#1 of 4** | **#1 of 4** | ✅ YES |
| python | py-django (3647 files) | **#1 of 4** | **#1 of 4** | ✅ YES |
| go | go-kubernetes/pkg (3125 files) | **#1 of 4** | **#1 of 4** | ✅ YES |
| java | java-spring (10142 files) | **#1 of 4** | **#1 of 4** | ✅ YES |
| csharp | csharp-efcore (2677 files) | **#1 of 4** | **#1 of 4** | ✅ YES |
| typescript | ts-typescript (723 files) | **#1 of 4** | **#1 of 4** | ✅ YES |

**PERF HOLDS across every language, no exceptions, no collapse.**
ctx-optimize is #1 on BOTH cold-gather speed and on-disk footprint on all 6
big real-world repos, spanning 723 to 10,142 files. Margins widen at scale,
not shrink: java-spring (biggest corpus) is where ctx-optimize's edge over
its nearest rival (CodeGraph) is largest in absolute terms (10.1s vs 19.2s
cold, 135MB vs 686MB store), and its edge over the slowest rival (GitNexus)
is 23× on cold-gather time (10.1s vs 234.5s) and 12× on store size.

## Where ctx-optimize LOSES — stays visible, not buried

- **Query latency**: CodeGraph's symbol-search `query <term>` beats
  ctx-optimize's free-text `query "<question>"` on 3 of 6 corpora
  (c-postgres 67ms vs 83ms, go-kubernetes 65ms vs 66ms — near tie, java-spring
  71ms vs 221ms — the clearest CodeGraph win). **This is not apples-to-apples**:
  CodeGraph's cell is a single keyword symbol-name search
  (`heap`/`queryset`/`scheduler`/`bean`/`tracker`/`checker`), ctx-optimize's is
  a free-text natural-language question, same asymmetry the small-corpus
  AUDIT.md already flagged (Issue 5) — kept honestly asymmetric here too,
  not hidden behind a relabeled "same question."
- **Warm/sync speed, EVERY corpus, no exceptions**: once primed,
  ctx-optimize's `up` 0-change no-op *is* fast (0.03–0.10s, on par with
  CodeGraph's sync). But on the `resync_1file_s` cell — one real file
  changed and committed — **ctx-optimize has NO incremental fast-path**:
  it re-extracts at essentially cold-gather cost every time (c-postgres
  5.35s resync vs 4.53s cold; java-spring 11.76s resync vs 10.14s cold).
  CodeGraph's `sync` on the same 1-file change is dramatically faster
  (java-spring: 2.31s vs ctx-optimize's 11.76s — a genuine incremental path).
  This confirms, at scale, exactly what AUDIT.md Issue 6 already flagged on
  the small corpora: **ctx-optimize has no incremental re-gather today.**
  Full resync table (seconds, all after a 1-file commit + revert):

  | corpus | ctx-optimize | codegraph | gitnexus | graphify |
  |---|---:|---:|---:|---:|
  | c-postgres | 5.35 | **0.29** | 37.74 | 32.20 |
  | py-django | 1.42 | **0.75** | 13.28 | 10.74 |
  | go-kubernetes | 4.58 | **0.71** | 43.07 | 29.55 |
  | java-spring | 11.76 | **2.31** | 238.71 | 170.86 |
  | csharp-efcore | 2.36 | **0.95** | 19.93 | 31.10 |
  | ts-typescript | 1.99 | **0.29** | 13.17 | 22.23 |

  CodeGraph wins the resync cell on **all 6 corpora**, every time, by
  4–20×. This is the single most consistent hole in ctx-optimize's story in
  this dataset and must stay visible wherever "warm"/"incremental" is
  discussed on the site.

## Ambiguous-symbol resolution finding (NOT the R1 import-stub pattern, but adjacent)

The R1 import-stub-hijack bug (from `2026-07-24-answer-quality/proposal.md`)
is specifically about a Python import alias shadowing the real definition.
**It did not reproduce verbatim in any of the 5 non-Python big corpora** —
but a related, real problem DID surface in Go:

`ctx-optimize card New` on go-kubernetes (searching for
`scheduler.New` in `pkg/scheduler/scheduler.go:253`) silently returned a
**different, unrelated** `New` function —
`controller/nodeipam/ipam/cidr_allocator.go:118` — with **no fuzzy-match
disclosure, no ambiguity warning**. Contrast with `card Run` on the same
corpus, which correctly refused ("has no exact match and several near names
score alike — refusing to guess") and listed 5 ranked candidates. The
difference: `Run` has *no* exact top-level match (only near-miss method
names), so the refusal path fires; `New` has *multiple exact* top-level
matches across different packages (a very common Go idiom — many packages
define their own `New`), and card's resolver picks one **silently** with no
disclosure at all. This is a real gap in the "honest resolution" design
documented in the CLI help ("fuzzy TIE refuses... exact/fuzzy resolution is
honest by default") — the honesty guarantee currently only covers the FUZZY
path, not exact-name collisions, which are arguably the more dangerous case
because there's no "[resolved via fuzzy → id]" flag telling the caller to
double check. Raw evidence:
`~/ctx-bench-arena/multilang/quality-big/go-kubernetes/ctx-optimize-card__New.txt`
vs `...ctx-optimize-card__Run.txt`. This deserves its own ADR-tracked follow-up
— flagging here as a benchmark finding, not fixing in this task.

## Symbols used for quality capture (grepped from real source, not invented)

| corpus | symbols |
|---|---|
| c-postgres | `heap_insert` (heapam.c:1837), `heap_update` (heapam.c:2993), `ReadBuffer` (bufmgr.c:708) |
| py-django | `QuerySet` (query.py:293), `Model` (base.py:459), `Manager` (manager.py:176) |
| go-kubernetes | `New` (scheduler.go:253), `Run` (scheduler.go:435), `applyDefaultHandlers` (scheduler.go:111) |
| java-spring | `DefaultListableBeanFactory`, `AnnotationConfigApplicationContext` |
| csharp-efcore | `ChangeTracker` (ChangeTracker.cs), `DbContext` (DbContext.cs) |
| ts-typescript | `createTypeChecker` (checker.ts:1446) |

## Tool failures

None of the 4 gather-capable tools (ctx-optimize, codegraph, gitnexus,
graphify) failed or timed out on any of the 6 big corpora — every cell
completed within the 600s cap, even gitnexus's slowest cell (java-spring,
234.5s). ast-grep's query cell returned real (rc=0, non-empty) matches on
all 6 languages after pattern verification.

## Reproduction

```
cd /Users/muthuishere/muthu/gitworkspace/ctx-optimize-bench-wt
task build   # or: go build -o bin/ctx-optimize ./cmd/ctx-optimize
python3 benchmarks/multilang/bench_multilang_big.py       # cold/warm/query, writes results-multilang-big.json
python3 benchmarks/multilang/warm_resync_fix.py            # up-verb warm + 1-file resync backfill
python3 benchmarks/multilang/quality_capture_big.py         # verbatim quality-capture dumps
```

Arena (clones + stores + logs, not committed): `~/ctx-bench-arena/multilang/`.
Quality-capture raw dumps: `~/ctx-bench-arena/multilang/quality-big/`.
