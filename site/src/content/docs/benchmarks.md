---
title: Benchmarks
description: "The graded agent run, the gather numbers, and the provenance of every cell — including the ones we cannot pin."
---

Two kinds of number live here, and they are not interchangeable: **what we measured
ourselves** with a committed harness, and **what we measured against other tools**, whose
provenance is weaker and is labelled as such.

## The graded agent run

Corpus **gorilla/mux** — small, modern, well-named Go, the terrain where plain grep is
already strong. Three arms, one clone each so no arm can read another's artifacts. Twelve
hand-verified questions, **3 runs, n = 36 answers per arm**, model `gpt-4o-mini`,
deterministic grading — no LLM judge, the same rule for every arm.

| metric | shell (ripgrep) | ctx-optimize | graphify |
|---|---|---|---|
| Correct on "who calls / what breaks" | 29% | **79%** | — |
| Overall correctness | 35% | **67%** | 40% |
| "Where is X" questions | **47%** | 42% | — |
| Tool calls per run | 42.7 | **15.0** | — |
| Wall seconds per run | 65.5 s | **40.1 s** | — |
| Empty answers | 4 | **0** | — |
| Runs that hit the step cap with nothing | 3 | **0** | — |

**Grep wins "where is X" outright.** That is its question, and the row stays in the table.
What moves is the structural question — who calls this, what breaks if I change it — where
a resolved symbol beats a matching line.

The transcripts are in the repo:
[proof/agent/RESULTS-QUALITY.md](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/RESULTS-QUALITY.md).

## Gather and query, v0.14.0

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

## Memory

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

## The harnesses, all committed

| harness | what it measures |
|---|---|
| `benchmarks/bench.py` · `bench_multi.py` | single-pass cold and warm gather, query latency |
| `benchmarks/session/session.py` | the **multi-pass session** an agent actually pays: cold build → queries → scripted edit → incremental → queries again, with **staleness probes** so a tool that "re-indexes instantly" by doing nothing scores as *wrong* rather than fastest |
| `benchmarks/locbench/` | Loc-Bench — an external yardstick we do not control. We enter only its **retrieval** tier |
| `internal/golden/` | the hermetic gate: extraction snapshots, landmark facts, perf ceilings, and a judged 20-question scoreboard per corpus. Scores may only move up |

The 2026-07-24 run was a scratch script that was never committed, so its numbers could not
be reproduced from the repo. It has been retired rather than patched, and several published
figures were corrected in the process — including two that had flattered a *competitor*.
