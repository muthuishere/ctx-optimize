---
title: Benchmarks
description: "Where the numbers sit: quality, session cost, gather, query. One table each. Provenance in a line, not a memoir."
---

Four facts. The rest is in the repo.

## 1. Was the answer right

gorilla/mux · `gpt-4o-mini` · 12 questions · 3 runs · n = 36 per arm · no LLM judge · committed harness.

| | grep | **us** | Graphify |
|---|---|---|---|
| Correct | 35% | **67%** | 40% |
| Who calls this / what breaks (8 q) | 29% | **79%** | 42% |
| Where is X (4 q) | **47%** | 42% | 36% |
| Empty / step-cap / false claim | 4 / 3 / 0 | **0 / 0 / 0** | 0 / 0 / 1 |

Grep wins locate. We win structure. [Transcripts](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/RESULTS-QUALITY.md).

## 2. What the session paid

Same run as above (means per run):

| | grep | **us** | Graphify |
|---|---|---|---|
| Tool calls | 42.7 | **15.0** | 26.0 |
| Wall | 65.5 s | **40.1 s** | 43.1 s |
| $ (this model, this corpus) | $0.0051 | **$0.0040** | $0.0070 |

ripgrep wins every *call*. The grep agent still finishes 25 s later because it calls three times as often.

Tokens are a **smaller** sample (4 questions, one run, OpenRouter accounting): **−36% tokens, −31% $** vs grep on mini; Graphify used *more* tokens than grep. Same store on Claude Code **−0.2%**, Codex **+3.0%**. Say both. Never “saves tokens” with no harness.

## 3. Gather (ours, v0.14, pinned goldens)

| corpus | v0.13 | **v0.14** |
|---|---|---|
| Linux kernel (84k source files) | 124 s | **55 s** |
| Kubernetes | 14.6 s | **7.3 s** |
| java-spring | 6.1 s | **2.9 s** |

`card` on that kernel store: **&lt;20 ms** (fail-safe index). `query` still walks every node (~3.5 s on linux). [How that ranking works](/ctx-optimize/concepts/#why-query-is-not-grep--and-not-a-vector-store).

## 4. Against the field

One copy of the competitor table lives on [compare](/ctx-optimize/compare/). The shape:

- **Small corpora, pin-verified (2026-08-15):** we lead cold, warm, query, and disk vs CodeGraph, Graphify, GitNexus (253–1,474 files).
- **Linux kernel (unpinned arena):** we gather faster (118 s vs CodeGraph 290 s vs Graphify 528 s; GitNexus DNF). They **query** faster (CodeGraph 0.79 s, rg 1.59 s, us 3.70 s). On five judged questions we were useful **4/5**, CodeGraph **0/5**, Graphify **0/5**.
- **Do not mix 55 s (v0.14, us vs us) into the 118 s competitor row.** Different runs.

Kernel competitor rows get a pinned re-run before anyone treats them as a launch number. The arena on this machine predates `setup.py` / `versions.json` for those cells.

## Harnesses (if you want to re-run)

`benchmarks/suite/tools.json` is the field. `bench.py` / `bench_multi.py` — one pass. `session/session.py` — the session an agent pays. `internal/golden/` — floors may only move up.
