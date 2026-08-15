---
title: Benchmarks
description: "Answer quality first, then round trips and wall time, then gather and query — and the provenance of every cell, including the ones we cannot pin."
---

One page, in the order the numbers matter: **was the answer right**, then **how many round
trips and how long it took**, then **how fast the index itself is**, then **how that stands
against other tools**.

Two kinds of number live here, and they are not interchangeable: **what we measured
ourselves** with a committed harness, and **what we measured against other tools**, whose
provenance is weaker and is labelled per row.

## 1. Answer quality — the graded agent run

*Provenance: corpus gorilla/mux; three arms, one clone each so no arm can read another's
artifacts; 12 hand-verified questions; **3 runs, n = 36 answers per arm**; model
`gpt-4o-mini`; graded deterministically — no LLM judge, the same rule applied to every arm,
and the grader never saw which tool produced the answer. Harness and transcripts committed.*

| quality metric | shell (ripgrep) | ctx-optimize | graphify |
|---|---|---|---|
| Overall correctness | 35% | **67%** | 40% |
| Impact — "who calls this / what breaks" (8 q) | 29% | **79%** | 42% |
| Locate — "where is X" (4 q) | **47%** | 42% | 36% |
| Empty answers | 4 | **0** | 0 |
| Runs that hit the step cap with nothing | 3 | **0** | 0 |
| False claims | 0 | **0** | 1 |

**Grep wins "where is X" outright.** That is its question, and the row stays in the table.
What moves is the structural question — who calls this, what breaks if I change it — where
a resolved symbol beats a matching line.

**The mechanism, in one pair of transcripts.** On *"which functions call
`requestWithVars`?"* the shell arm ran one `grep -r`, got matching LINES, named the right
files, could not name a single caller, and invented the line numbers — it printed "line 4"
and "line 3", which are grep's ordinal output positions; the real sites are `mux.go:209`
and `test_helpers.go:18`. The store arm ran `affected requestWithVars` once and returned
`Router.ServeHTTP` (mux.go:188-229) and `SetURLVars` (test_helpers.go:17-19).

Two caveats we print with it every time. **Part of the gap is cheap-model weakness, not a
tool ceiling** — `gpt-4o-mini` flounders on long grep transcripts, ran `grep -r` without
`-n`, and on one question burned 15 steps on malformed commands and returned nothing. And
the run surfaced **two defects of ours that are still open**: ambiguous method names
collapse the call graph (`Match` is defined 8 times in mux, the AMBIGUOUS rule filters those
edges out, and `affected "Route.Match"` returns only the containing file), and `query`
ranking misfires on conceptual phrasing — which is exactly why it loses locate questions to
plain grep.

The transcripts are in the repo:
[proof/agent/RESULTS-QUALITY.md](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/RESULTS-QUALITY.md).

## 2. Round trips, tokens, and wall time

*Provenance: the same 3 runs / n = 36 per arm described in section 1; tool calls, wall
seconds and cost are per-run means recorded by the harness.*

| efficiency metric | shell (ripgrep) | ctx-optimize | graphify |
|---|---|---|---|
| Tool calls per run | 42.7 | **15.0** | 26.0 |
| Wall seconds per run | 65.5 s | **40.1 s** | 43.1 s |
| Cost per run | $0.0051 | $0.0040 | $0.0070 |

This is the counter-intuitive part, and it is why per-call latency is the wrong unit:
**ripgrep wins every individual call, and the grep-driven agent still finishes 25 seconds
later**, because it greps, reads a file, greps again, chases a caller, re-reads. Roughly
three times the round trips.

### Tokens and cost — a separate, much smaller run

*Provenance, and read it before the numbers: **4 questions, ONE run, one corpus
(gorilla/mux), one model** (`gpt-4o-mini` via OpenRouter). Tokens and cost are OpenRouter's
own accounting (`usage.include=true`), not estimates. This is a far smaller sample than the
correctness figures in section 1 — those are 12 questions over 3 runs, n = 36 per arm. The
two do not share a sample size and should not be read as if they do. Summary committed at*
[proof/agent/results/SUMMARY-gorilla-mux.md](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/results/SUMMARY-gorilla-mux.md),
*per-question JSON beside it.*

| totals over 4 questions | shell (ripgrep) | ctx-optimize | graphify |
|---|---|---|---|
| Tokens | 15,078 | **9,659** (−36%) | 18,352 (+22%) |
| Cost | $0.0024 | **$0.0016** (−31%) | $0.0025 (+7%) |
| Steps | 11 | **4** (−64%) | 8 (−27%) |
| Wall time | 16.5 s | **11.8 s** (−29%) | 18.7 s (+13%) |

**graphify used more tokens than plain ripgrep** on this set, and cost more. That is in the
same committed file as our own number.

:::caution[The same store is parity on frontier harnesses — say both, always]
On **Claude Code the store moved token usage by −0.2%, and on Codex by +3.0%**: parity at
equal quality, recorded in
[CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md).

Both results are ours and both are true, because of the mechanism. In a **thin loop**
retrieval *is* most of the context, so cutting 11 steps to 4 cuts tokens roughly in
proportion. On **Claude Code and Codex** the agent's own fixed costs — system prompt,
reasoning tokens, the answer itself — dominate the bill, and no retrieval tool shrinks
those.

So: **36% fewer tokens and 31% cheaper on a small-model harness, and parity on Claude Code
and Codex.** The claim that is dead is the *universal* one —
[what we do not claim](/ctx-optimize/limits/#-saves-you-tokens-universally).
:::

## 3. Gather and query, v0.14.0

Measured with the committed harness on pinned golden corpora, best-of-3:

| corpus | v0.13.0 | v0.14.0 |
|---|---|---|
| Linux kernel (84k files) | 124 s | **55 s** |
| Kubernetes | 14.6 s | **7.3 s** |
| java-spring | 6.1 s | **2.9 s** |

A one-file re-gather went from 98% of a cold build to **86%**. Most of the win was not where
anyone assumed: `store.Replace` ran once **per producer**, each a full read-sort-write of the
entire graph — and the store's own cost turned out to be **sorting, not parsing** (1,375 ms
of 2,624 ms on Kubernetes), because we were re-sorting a file we had written sorted
ourselves.

`card` on the Linux kernel answers in **under 20 ms**, via a plain sorted-text index that
is 20% of the graph and **fails safe**: on any staleness mismatch the caller falls back to a
full scan. It can make an answer fast; it cannot make one wrong.

## 4. Memory, and where the cap stops working

| tree | before | after |
|---|---|---|
| 7-module monorepo | 12.4 GB | **3.42 GB** (0.73 GB at `GOMAXPROCS=2`) |
| Kubernetes | — | 2.4× lever from `GOMAXPROCS` |
| Linux kernel | 14.65 GB | 14.36 GB — **no cap at all** |

Worker pools used to key on `runtime.NumCPU()`, which ignores cgroup quotas: a container
limited to 2 CPUs on a 64-core host spawned 63 wasm instances and OOMed *despite its quota*.
They now key on `GOMAXPROCS` and draw from a process-wide budget.

**Where that cap stops working, stated plainly:** it bounds wasm instances, not the graph.
On the full kernel (2.85M nodes / 5.54M edges) the resident cost *is* the graph, so treat
**~14 GB as a floor** for kernel-scale trees — a 16 GB machine is marginal and an 8 GB
machine cannot gather it.

## 5. Against other tools

The head-to-head tables live on the [comparison page](/ctx-optimize/compare/) and are not
duplicated here — one copy, one place to correct.

What is worth carrying across is the shape of that board. On the **pin-verified**
2026-08-15 run over 253–1,474-file corpora we lead cold gather, warm re-run, query latency
and store size against CodeGraph, graphify and GitNexus
([the table](/ctx-optimize/compare/#2-re-run-2026-08-15--the-first-pin-verified-run)). On
the **Linux kernel we lose the latency column outright** — CodeGraph answers in 0.79 s and
ripgrep in 1.59 s against our 3.70 s — and on the same five questions CodeGraph was useful
on 0 of 5 to our 4 of 5, with one of ours wrong
([the table, and its caveats](/ctx-optimize/compare/#3-measured-head-to-head--codegraph-gitnexus-graphify-ripgrep-2026-08-run)).
**Those kernel rows are not pin-verified** — see below.

:::caution[Provenance of the competitor numbers]
**As of 2026-08-15 the arena is pin-verified — for the first time.**
`benchmarks/suite/setup.py` rebuilds the field from `tools.json` with shallow *pinned*
clones and writes `versions.json`; the runner now copies that provenance into every result
file. The 2026-08-15 small-corpus run on the
[comparison page](/ctx-optimize/compare/) carries held pins for
[CodeGraph](https://github.com/colbymchenry/codegraph),
[graphify](https://graphify.com/) and
[GitNexus](https://github.com/abhigyanpatwari/GitNexus), and records
[CodeGraphContext](https://github.com/Shashankss1205/CodeGraphContext) as `pinned: false`
rather than quietly reporting it as pinned.

**The kernel-scale rows are still unpinned.** They come from the earlier arena and are
labelled by the build that produced them. They will be re-run before any of them is used
as a headline.

Getting there fixed three real defects in our own harness: `setup.py` crashed before it
could write `versions.json` at all, a Homebrew interpreter's PEP 668 refusal meant Python
competitors were being run from whatever was already on PATH rather than from the pinned
clone, and a filtered `--tools` run *overwrote* the provenance file instead of updating it,
leaving a results file that claimed exactly one pinned competitor. GitNexus's recorded
build command also turned out to match a layout the pinned tree does not have — so the
binary in the arena had not been built from the pin either.
:::

## 6. The harnesses, all committed

| harness | what it measures |
|---|---|
| `benchmarks/bench.py` · `bench_multi.py` | single-pass cold and warm gather, query latency |
| `benchmarks/session/session.py` | the **multi-pass session** an agent actually pays: cold build → queries → scripted edit → incremental → queries again, with **staleness probes** so a tool that "re-indexes instantly" by doing nothing scores as *wrong* rather than fastest |
| `benchmarks/locbench/` | Loc-Bench — an external yardstick we do not control. We enter only its **retrieval** tier |
| `internal/golden/` | the hermetic gate: extraction snapshots, landmark facts, perf ceilings, and a judged 20-question scoreboard per corpus. Scores may only move up |

The 2026-07-24 run was a scratch script that was never committed, so its numbers could not
be reproduced from the repo. It has been retired rather than patched, and several published
figures were corrected in the process — including two that had flattered a *competitor*.
