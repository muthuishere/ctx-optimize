---
title: Compared with other tools
description: "Where ctx-optimize stands against CodeGraph, GitNexus, graphify and ripgrep — including where they win."
---

This page is written the way the rest of this site is: **if a competitor is better at something, it says so.** ctx-optimize is a fresh entrant in a lane that already has two 40K-star leaders. We think our design choices are right — but you should see the whole board, including where we lose, before you decide.

**The short version:** we are not the fastest to answer — we are the one that answers, and the agent finishes sooner because it asks fewer times. CodeGraph replies to a kernel query in 0.88 s to our 3.92 s and scored 0 of 5 on relevance; ripgrep is faster still and returns a different artifact entirely. Per *question* rather than per call, the graded run has the store arm at **40.1 s against a shell agent's 65.5 s**. Relevance table: [section 2](#relevance). Graded run: [section 3](#graded).

## The category converged on one pattern: a local graph, served over MCP.

A year ago "give your agent codebase context" meant embeddings and a vector DB. It doesn't anymore. The tools that broke out — **CodeGraph** (~47K stars, MIT) and **GitNexus** (~42K stars) — both pre-compute a structural graph **on-device** and expose it to agents over the **Model Context Protocol (MCP)**: no cloud, no embeddings API, no code leaving the machine. **graphify** popularized the central-store idea; **potpie** took the funded, Neo4j-backed platform route; **Serena** skips the graph entirely and wraps live language servers. We share the local-first, deterministic half of that consensus — and we make one deliberately contrarian choice, spelled out below.

:::note
**Numbers are approximate, mid-2026, and move fast.** Star counts and feature lists here are from public repos and third-party write-ups (linked at the bottom), not our benchmarks. The measured head-to-head is in section 2 below; where a row is measured it replaces the claimed one, wins and losses alike.
:::

## Where everyone stands, as honestly as we can draw it.

|  | ctx-optimize | CodeGraph | GitNexus | graphify | potpie | Serena |
|---|---|---|---|---|---|---|
| Adoption (approx) | new | ~47K★ | ~42K★ | ~82K★ | funded | established |
| Runtime | one static Go binary | SQLite | embedded DB | Python | Neo4j service | language servers |
| Setup deps | zero | zero (1 file) | zero | model key | DB + infra | LSP per lang |
| Distribution | agent skill + hook | MCP (42 tools) | MCP (16 tools) | skill | API | MCP |
| Agent reach | CC / Codex / Copilot / Devin | 8 hosts via MCP | MCP hosts | skill | API | MCP hosts |
| Determinism | no model, ever | structural | structural | model-assisted | LLM in loop | LSP-exact |
| Extraction engine | tree-sitter AST | AST | AST | AST | AST | language server |
| Call-edge precision | AST + conservative name-resolve | AST name-resolve | AST name-resolve | AST name-resolve | AST name-resolve | LSP type-exact |
| Freshness / staleness gate | git-HEAD gate | auto-sync | sync | static dump | re-index | live |
| Routes / deps / k8s as graph | yes (v0.3) | routes+bridges | partial | no | no | no |
| Extensible without forking | packs (4 axes) | config | config | plugins | no | no |
| Module = scattered folders (src/ + tests/) | yes (v0.3.5) | one graph | one graph | one graph | — | — |
| Team clone → prebuilt graph in one step | one verb: `up` | rebuild | rebuild | rebuild | re-index | re-warm |
| License | MIT | MIT | noncommercial | open | open-core | MIT |

green = a genuine strength, amber = partial / caveated, red = a real weakness. Notice ours has red in it. The most important red is **MCP** — see "our bet."

## Two kinds of numbers: what we measured, and what's structurally true of everyone.

The measured tables below open with the questions we lost and the tools that beat us to the reply. That order is deliberate — a scoreboard you only publish when you're winning isn't a scoreboard.

### 1. Setup & footprint — true for every rival, by architecture

You don't need a benchmark to count dependencies. This is the win we hold against the *whole* field, not just one tool — the deterministic single binary carries none of the runtime cost a graph-DB, a model, or a language-server does:

|  | ctx-optimize | CodeGraph | GitNexus | graphify | potpie | Serena |
|---|---|---|---|---|---|---|
| Services to run | 0 | SQLite (embedded) | embedded DB | Python runtime | Neo4j | LSP / language |
| Model / API key | none | none | none | for labeling | LLM in loop | none |
| Install | 1 static binary | binary + index | binary | pip + deps | containers | server + LSPs |
| Works fully offline | yes | yes | yes | not for labeling | no | yes |
| Cold start → first answer | seconds | seconds | seconds | index + model | DB bring-up | LSP warmup |

### 2. Measured head-to-head — CodeGraph, GitNexus, graphify, ripgrep (2026-08 run)

:::note
**Correction — this page previously overstated CodeGraph by 4×.** The 2026-07-24 tables published CodeGraph's flask query at **416 ms**. That number was wrong and it flattered us. CodeGraph resolves its store from the working directory, and our old harness invoked it from the wrong one; the real figure is **102 ms**. Because the same harness bug touched *every* CodeGraph cell in that run, the whole 2026-07-24 head-to-head has been retired rather than patched, and replaced below with a clean re-run. The warm-re-sync and store-on-disk tables from that run are gone with it — they will return when they are re-measured. A "fastest" claim published next to a number that overstates a competitor is not a fastest claim; it is a mistake with a bar chart on it.
:::

:::note
**Correction — CodeGraph's kernel query was published at 536 ms. That figure is wrong.** In that run CodeGraph was answering a *single word* while every other tool answered the full question phrase — not the same workload, so not a comparable number. The correct figure for the phrase every tool received is **880 ms**, and it is what appears in the table below. The correction makes CodeGraph look *slower* than we previously printed and it is still 4.5× faster than us on that question; we are publishing it because a number produced by an unequal workload is not a benchmark. (The separate 2026-07-24 **416 ms → 102 ms** CodeGraph correction is above and still stands.)
:::

Same machine (Apple M5 Pro, 18 cores, 48 GB), every tool on its own fastest deterministic path — no embeddings, no LLM, no clustering, for anyone. Versions: **ctx-optimize — the v0.12.0 binary plus the symbol-index work, i.e. the build that became v0.13.0** (released 2026-08-05); these figures have not been re-run against the tagged v0.13.0 artifact, so they are labelled by the build that produced them rather than by a version they were not taken on. CodeGraph 1.5.0 · graphify 0.9.12 · GitNexus 1.6.9 · ripgrep 15.2.0. Kernel questions are median-of-3; small-corpus gather is best-of-3 and query median-of-5.

A latency column on its own is not a comparison, so this table carries the answer too. **Grading rule: does the top hit name a symbol actually related to the question, with a `file:line`?** ripgrep returns matching lines — a different artifact — so it is marked **n/a** and never scored against a symbol rule.

| question | ctx-optimize | CodeGraph | graphify | ripgrep |
|---|---|---|---|---|
| mq deadline dispatch request | 3.92 s · USEFUL `dd_dispatch_prio_aged_requests` block/mq-deadline.c:564 | 0.88 s · NOT `struct request` | 23.10 s · NOT `u64` x_tables.h:508 | 1.64 s · n/a lines only |
| ext4 write iter | 3.54 s · USEFUL `ext4_buffered_write_iter` fs/ext4/file.c:285 | 0.78 s · NOT `function iter` | 22.45 s · NOT `u32` x_tables.h:507 | 1.58 s · n/a lines only |
| spinlock irqsave | 3.80 s · USEFUL `__raw_spin_lock_irqsave` spinlock_api_smp.h | 0.59 s · NOT `struct spinlock` | 23.07 s · NOT `u32` x_tables.h:507 | 1.58 s · n/a lines only |
| tcp congestion control | 3.70 s · USEFUL `proc_tcp_available_congestion_control` | 0.79 s · NOT `struct tcp` | 23.69 s · NOT `u64` x_tables.h:508 | 1.64 s · n/a lines only |
| page allocation failure | 3.60 s · NOT `MLX5_…ALLOCATION_FAIL` | 0.81 s · NOT empty result | 22.77 s · NOT `kcalloc()` ptr_ring.c:50 | 1.59 s · n/a lines only |
| **median / useful** | 3.70 s · 4 of 5 | 0.79 s | 23.07 s · 0 of 5 | 1.59 s · n/a |

**Say it plainly: CodeGraph is 4.7× faster than us and got 0 of 5.** Its top hits were bare type names — `struct request`, `struct tcp`, `function iter` — and one question returned nothing at all. **graphify returns `u64`/`u32` from netfilter/`x_tables.h` for four different questions**: that is a constant, not an answer, at 23 seconds a call. **ripgrep is 2.3× faster than us** and returns matching lines, which is why it is n/a rather than scored. And **we got 1 of 5 wrong ourselves**.

:::note
**Caveat, printed with the table:** this is a **5-question judged sample against a stated rule**, not a blind graded run, graded by the author on a corpus we also tune against. The blind, multi-run, deterministically graded study is [section 3 below](#graded) (12 questions, 3 runs, n = 36 per arm) — and there **grep beats us on locate questions**, 47% to 42%.
:::

|  | gather | nodes | store |
|---|---|---|---|
| ctx-optimize | 118.18 s | 2,849,719 | 2.0 GB |
| CodeGraph 1.5.0 | 289.86 s | 1,838,442 | 4.1 GB |
| graphify 0.9.12 | 527.72 s | 910,778 | 3.1 GB |
| GitNexus 1.6.9 | did not finish | — | — |

**GitNexus did not finish in 45 minutes** — 137 CPU-minutes, a 36 GB heap, and no index produced. We report that as a non-finish, not as a win and not as a multiplier: a tool that never produced an artifact has no time to compare against. † **graphify cannot query this corpus at default settings at all.** It builds a 1.2 GB `graph.json` and then errors with `exceeds 536_870_912-byte cap`; its 23 s query figures above are only reachable after raising `GRAPHIFY_MAX_GRAPH_BYTES` by hand. Each tool builds a *different* graph — node counts and store sizes compare each tool's own artifact, not identical outputs.

| corpus | ctx-optimize | CodeGraph | graphify | GitNexus |
|---|---|---|---|---|
| flask (344 files) | 0.314 | 0.438 | 0.845 | 6.355 |
| gin (253) | 0.342 | 0.593 | 0.777 | 7.56 |
| ctx-optimize src (409) | 0.326 | 0.762 | 1.352 | 12.85 |
| graphify src (1,476) | 0.648 | 1.323 | 5.123 | 10.649 |

| corpus | ctx-optimize | ripgrep ‡ | CodeGraph | graphify | GitNexus |
|---|---|---|---|---|---|
| flask (344 files) | 12 | 11 | 102 | 106 | 794 |
| gin (253) | 11 | — | — | 110 | — |
| ctx-optimize src (409) | 12 | — | — | 120 | — |
| graphify src (1,476) | 27 | 23 | 101 | 373 | 779 |

— = not measured in this run; empty cells stay empty. Note the corpora are re-counted against the current pinned SHAs (flask 344, gin 253, ctx-optimize src 409, graphify src 1,476) — do not compare these file counts against the older run's.

:::note
**‡ ripgrep beats us, and we are saying so first.** 11 ms against our 12 on flask; 23 against our 27 on graphify src; 1.59 s against our 3.70 s on the kernel. Claude Code's Grep tool *is* ripgrep, so that is the baseline every agent already carries. It is also a different question. **rg returns matching lines; we return resolved symbols** — declaration, signature, `file:line`, callers, and blast radius. `rg` is the fastest way to find a string, and it cannot tell you who calls a function. That is the whole trade, and it is why **we do not claim to be the fastest anything**: we are the slowest graph in our own kernel table and the only one that named the function.
:::

### 3. Answer quality — the graded run

Speed tables cannot tell you whether an answer was right, so there is a separate graded study: **gorilla/mux, 12 hand-verified questions, 3 runs, n = 36 per arm**, deterministically graded (no LLM judge, same rule for every arm), comparing a shell agent (ripgrep), the same agent with ctx-optimize, and the same agent with graphify, all on `gpt-4o-mini`.

**This is also where the per-query latency tables above stop being the interesting unit.** ripgrep wins every individual call — and a grep-driven agent needs roughly three times as many of them, because it greps, reads a file, greps again, chases a caller, re-reads. **End to end the store arm finished in 40.1 s against the shell arm's 65.5 s — 1.6× faster overall — out of individually slower queries.** Per-call speed is not what decides whether the agent answers correctly, or when it finishes.

| metric | shell (ripgrep) | ctx-optimize | graphify |
|---|---|---|---|
| Tool calls per run | 42.7 | 15.0 | 26.0 |
| Wall seconds per run | 65.5 | 40.1 | 43.1 |
| Correctness | 35% | 67% | 40% |
| Impact — "who calls this" (8 q) | 29% | 79% | 42% |
| Locate — "where is X" (4 q) | 47% | 42% | 36% |
| False claims | 0 | 0 | 1 |
| Empty answers | 4 | 0 | 0 |
| Hit the step cap (no answer) | 3 | 0 | 0 |
| Cost per run | $0.0051 | $0.0040 | $0.0070 |

**The mechanism, in one pair of transcripts.** On *"which functions call `requestWithVars`?"* the shell arm ran one `grep -r`, got matching LINES, named the right files, **could not name a single caller**, and invented the line numbers (it printed "line 4" and "line 3" — grep's ordinal output positions; the real sites are `mux.go:209` and `test_helpers.go:18`). The store arm ran `affected requestWithVars` ONCE and returned `Router.ServeHTTP` (mux.go:188-229) and `SetURLVars` (test_helpers.go:17-19). Full transcripts: [RESULTS-QUALITY.md](https://github.com/muthuishere/ctx-optimize/blob/main/proof/agent/RESULTS-QUALITY.md).

Caveats we print with it every time. **grep wins the locate questions** — 47% to our 42%; "where is X" is what a string search is for. **Part of the gap is cheap-model weakness, not a tool ceiling**: `gpt-4o-mini` flounders on long grep transcripts, ran `grep -r` without `-n`, and on one question burned 15 steps on malformed commands and returned nothing. And the cost row — $0.0040 against $0.0051 — is what *this* run cost on *this* corpus with *this* model. It is not a token-savings claim and never becomes one; see the note at the end of this section.

**Two defects of ours the run surfaced, still open.** **(1)** Ambiguous method names collapse the call graph — `Match` is defined 8 times in mux, the AMBIGUOUS rule filters those edges out, and `affected "Route.Match"` returns *only the containing file*. **(2)** `query` ranking misfires on conceptual phrasing, which is why it loses locate questions to plain grep.

potpie and Serena still have no measured row — different architectures (Neo4j + LLM-in-loop; LSP server) that need their own harness, and we won't fake a head-to-head we haven't run. Raw data behind ours: [proof/agent](proof/agent/) · [bench/results.json](bench/results.json) · [bench/results-multi.json](bench/results-multi.json). Note `results-multi.json` is the retired 2026-07-24 harness output — it contains the CodeGraph cells corrected above and is kept only as a record of the mistake.

:::note
**One claim we will never make again: token savings.** It was measured and killed — on frontier harnesses the store moved agent token usage by **−0.2% (Claude Code)** and **+3.0% (Codex)**, i.e. parity at equal quality. Agent fixed costs don't shrink with a better tool. The record is public in [CRITIQUE.md](https://github.com/muthuishere/ctx-optimize/blob/main/docs/CRITIQUE.md). What moved is **tool calls per run**, above.
:::

## What each competitor is genuinely better at — and where we'd pick ourselves.

### CodeGraph

********

### GitNexus

### graphify

****``

### potpie & Serena

********

## A single deterministic binary you extend, not a service you run.

Strip away the scoreboard and the difference is philosophical. Everyone else is a **store format + a server** (SQLite, LadybugDB, Neo4j) or a **live service** (LSP). We're a **single static Go binary** — no database process, no embeddings, no model, no network except what you invoke yourself (a self-update, a one-time toolchain fetch, your own remote scripts) — whose artifacts are plain, sorted, git-diffable files. What that buys:

- +**Truly zero setup.** `npm i -g` or one binary, works offline, nothing to stand up — not even a local DB daemon.

- +**Extend without forking.** Routes, manifests, languages, and any data source are drop-in packs/adapters — commit the file, the whole team's agents inherit it. No other tool here lets you add a framework recognizer as a committed JSON file.

- +**Honest provenance + freshness.** Every edge is `EXTRACTED` (parsed) or `INFERRED` (name-matched); `fresh` gates answers against git HEAD so the agent never trusts a stale graph.

- +**More than code.** Routes→handlers, dependencies that federate across build tools, k8s topology, git co-change, community subsystems — in one graph, one query.

- ±**Agent skill over MCP — a deliberate bet.** The leaders distribute over MCP; we bet on the **agent-skill + hook + committed pointer** path instead. A skill is richer than a fixed MCP tool list — it teaches the agent *when* and *how* to use the store, carries the query-craft rules, and drives onboarding and customization end-to-end. The tradeoff, stated plainly: on MCP-only hosts (Cursor, Kiro, Gemini CLI) you'd wire the CLI in yourself — there's no MCP server, by choice, and there won't be one. On Claude Code, Codex, Copilot and Devin the skill lands the moment you run `install --skills`.

- −**Call edges are conservative, not LSP-exact.** We parse with tree-sitter (real ASTs, same engine class as CodeGraph/GitNexus/graphify) and resolve a call to its declaration by unique name — **dropping ambiguous matches rather than guessing**, so there are no false edges. That's the same precision tier as the other graph tools; only Serena's language server is genuinely type-exact (it knows the exact overload). Closing that last gap (go/x/tools first) is on the roadmap.

:::note
**The honest summary:** if you want the most-adopted, MCP-everywhere tool today, that's CodeGraph or GitNexus. If you want a company-safe license, avoid GitNexus. If you want symbol-exact refactors, add Serena. If you want **one zero-dependency binary that indexes code + routes + dependencies + infra deterministically and lets your team extend it without a fork**, delivered to your agent as a skill rather than an MCP server, on Claude Code / Codex / Copilot / Devin — that's us. We'd rather earn the comparison than hide from it.
:::

**[](/ctx-optimize/)[](/ctx-optimize/cli/)[](https://github.com/muthuishere/ctx-optimize)[](https://rywalker.com/research/code-intelligence-tools)[](https://www.knolli.ai/post/graphify-alternatives)
