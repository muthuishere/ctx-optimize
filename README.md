# ctx-optimize

[![CI](https://github.com/muthuishere/ctx-optimize/actions/workflows/ci.yml/badge.svg)](https://github.com/muthuishere/ctx-optimize/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/@muthuishere/ctx-optimize?logo=npm)](https://www.npmjs.com/package/@muthuishere/ctx-optimize)
[![Go Reference](https://pkg.go.dev/badge/github.com/muthuishere/ctx-optimize.svg)](https://pkg.go.dev/github.com/muthuishere/ctx-optimize)
[![license](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Discord](https://img.shields.io/badge/AgentNexus-join%20the%20community-5865F2?logo=discord&logoColor=white)](https://discord.gg/V9C2kvHC8D)

**[muthuishere.github.io/ctx-optimize](https://muthuishere.github.io/ctx-optimize/)** — docs, demos, benchmarks, and the use-case tour.
**[Releases & changelog](CHANGELOG.md)** — what shipped in every version, newest first.

### Know what breaks before you change it.

One static Go binary gathers a repo — and, via native sources, its databases,
buckets, queues and APIs — into a local knowledge graph your agent answers
from in one call instead of a grep-and-read chain. On a graded benchmark it
answered **79% of "what calls this / what breaks if I change this" questions
correctly, against 29% for a grep-armed agent**. It indexes the **linux
kernel — 84,300 files — in 118 seconds**, 2.5x faster than the next tool, and
is the only one of four that produces a usable kernel graph at all.
Deterministic: no LLM, no embeddings, no database, no MCP, no credentials at
rest. The only intelligence in the system is the agent you already run.

Where it does **not** win: `ripgrep` is faster at finding a string and better
at "where is X" (47% vs 42%), and CodeGraph answers free-text kernel queries
in 880ms against our 4.0s — though on five kernel questions its top hit was
useful 0 times to our 4. Numbers for all of it in [Proof](#proof).

## Why

1. **Best at impact questions** — **79% vs 29%** (grep) and 42% (graphify) on
   8 graded "who calls this / what breaks" questions, 3 runs, n=36 per arm.
   Zero false claims and zero empty answers, in 15 tool calls per run against
   grep's 42.7. [Full method + failures](proof/agent/RESULTS-QUALITY.md).
2. **The agent finishes sooner even though each query is slower** — ripgrep
   answers a single lookup faster than we do (1.59s vs 3.70s on the kernel).
   But an agent does not make one call: on the graded run it made **42.7 tool
   calls per question with grep against our 15.0** — grep, read a file, grep
   again, chase a caller, re-read. End to end it finished in **40.1s with
   ctx-optimize against 65.5s with grep, 1.6x faster overall**, and answered
   67% correctly against 35%. Per-call latency is not the unit; the question is.
3. **Fastest to build, by a wide margin** — linux v6.9 in **118.18s** vs
   CodeGraph's 289.86s and graphify's 527.72s; GitNexus did not finish in 45
   minutes. A 1,476-file repo in **0.648s** vs 5.123s / 1.323s / 10.649s. And
   the graph is the most complete: 2,849,719 nodes in 2.0GB, against
   CodeGraph's 1,838,442 in 4.1GB.
4. **Instant symbol lookup** — `card` on the kernel resolves an exact symbol in
   **under 20ms** (it was 1.8s), via a plain-text index that is 20% of the
   graph. Fuzzy and ambiguous names still cost a full scan, deliberately: they
   rank against every node, and refusing to guess is the point.
5. **Complete answers, not pointer lists** — `card` returns signature + doc +
   callers/callees with `file:line`, so the agent doesn't reopen the file.
   `change-plan` composes the whole "I'm about to edit X" answer — callers,
   blast radius, which tests to run — in a single call. What we measured and
   kept: **impact-answer correctness**, onboarding traces, wall time. What we
   measured and dropped: token savings — Claude Code −0.2%, Codex +3.0%, so we
   don't claim it ([CRITIQUE.md](docs/CRITIQUE.md)).
6. **It maps the edges of the system, which no other tool we benchmarked does** —
   `boundaries` answers "what does this call out to, and what does it expose"
   in one command: external hosts, env vars with credentials flagged **by name**,
   spawned binaries, exposed routes — each with `file:line`. graphify, CodeGraph
   and GitNexus model none of it.

   ```
   $ ctx-optimize boundaries
   boundaries: 76 ports

   CONSUMES (what this system calls out to)
     config.env     30 · 30 external · 1 SENSITIVE · 3 dynamic
         OPENROUTER_API_KEY     INFERRED   SECRET  proof/agent/agent.mjs:L28 (+1 sites)
     network.http   17 · 17 external
         api.github.com         INFERRED    internal/app/app.go:L3149
     process.exec   12 · 12 external · 7 dynamic
         git                    AMBIGUOUS   internal/app/githistory_cli_test.go:L28 (+13 sites)

   PROVIDES (what this system exposes)
     network.http   17
         /api/graph             INFERRED    internal/dashboard/dashboard.go:L119

   UNRESOLVED  10 port(s) carry a dynamic identifier — os.Getenv(varName) and
               friends. The SITE is certain, the value is not.
   ```

   Read the tiers: a port list is a **floor, not a census**. `config.env` is
   INFERRED and `process.exec` is AMBIGUOUS because `os.Getenv(varName)` and
   `exec.Command(bin)` hide the value behind a variable — we report the site and
   refuse to invent the name. Two more limits worth stating: `scope` says
   `external` on every consumed port today (the internal/external join compares
   hosts against route paths and never matches), and `query` cannot retrieve
   these — use `boundaries` or `nodes --kind port`. `--json` carries the
   `otel.*` keys under their OpenTelemetry semconv names, so a static boundary
   joins a runtime trace on the same key.
7. **Your infrastructure goes in the graph too** — databases, buckets, queues
   and APIs enter by env-var **name**; the value is a URL and its scheme picks
   the connector. Nine of them: **postgres · mysql · mongodb · redis · kafka ·
   nats · s3 · mssql · openapi**. The secret value is never read into config,
   stored, or printed. [docs/sources.md](docs/sources.md)

   ```sh
   export BILLING_DB_URL='postgres://reader:$PG_PASS@db.internal:5432/billing'
   ctx-optimize add BILLING_DB_URL      # same door for s3, kafka, mongo, redis, nats, mssql, openapi
   ```
8. **Extensible without a fork** — languages are drop-in tree-sitter
   [grammar packs](docs/languages-packs.md) (12 embedded, `languages add` builds
   any other), external systems are [dropped scripts](docs/adapters.md) through
   one validated JSON door, and the [remote is your script](docs/remote-github.md).
   The store is plain sorted ndjson at `~/ctxoptimize/<repo>/` — diffable,
   portable, greppable.

## Install

**Already in Claude Code, Codex or Copilot? Paste this and it does the setup:**

```text
Install ctx-optimize for this repo:
1. npm install -g @muthuishere/ctx-optimize
2. cd into the repo root and run: ctx-optimize up
3. run: ctx-optimize install --skills

Then answer my code questions with its verbs — query, card, change-plan,
affected, boundaries — instead of grep-and-read, and cite the file:line it
returns.
```

Every line is a command you could have typed. No `curl | sh`, nothing behind a
shortener: you can read what will run before an agent runs it.

By hand:

```sh
npm install -g @muthuishere/ctx-optimize     # prebuilt binaries, macOS/Linux/Windows
go install github.com/muthuishere/ctx-optimize/cmd/ctx-optimize@latest   # or from source

ctx-optimize install     # skills + hooks for every agent CLI it detects
ctx-optimize update      # binary + skills + hooks; network only when YOU run it
```

## Quick start

```sh
ctx-optimize up                        # the only onboarding verb: bootstraps config,
                                       # pulls the team store or gathers. Idempotent.
ctx-optimize query "refund flow"       # complete, citable hits under a token budget
ctx-optimize change-plan RefundService # callers + blast radius + which tests to run
ctx-optimize serve                     # → 127.0.0.1:4747, embedded dashboard, zero external requests
```

## What you ask it

| You want to… | Verb |
|---|---|
| find something by intent | `query "<terms>"` |
| inspect one symbol (signature, doc, callers, callees) | `card <symbol>` |
| edit safely — callers, impact, tests, co-change | `change-plan <symbol>` |
| blast radius of a change | `affected <symbol>` |
| how are these two connected | `path <a> <b>` |
| why does this node exist / where from | `explain <node>` |
| the load-bearing symbols | `hubs` |
| what this calls out to & exposes — hosts, env vars, secrets, spawned binaries, routes | `boundaries` |
| list & filter without jq | `nodes --kind K` · `edges --relation R` · `deps --scope dev` |
| add a database / bucket / queue / API | `add BILLING_DB_URL` |
| feed anything else in | `<your-script> \| add --json -` |
| re-gather code only / run adapters only | `sync` · `adapters run [name]` |
| is my store trustworthy right now | `fresh` · `status --json` |
| share it with the team | `remote push` / `remote pull` |
| combine or dump the graph | `merge a b --into all` · `export --format dot` |
| a human-readable wiki of the codebase | `wiki` |
| browse it | `serve` |

Full reference with when-and-why for each: [docs/cli.md](docs/cli.md).

## Proof

Apple M5 Pro (18 cores, 48 GB). ctx-optimize on the build that became
v0.13.0, graphify 0.9.12,
CodeGraph 1.5.0, GitNexus 1.6.9, ast-grep 0.45.0, ripgrep 15.2.0. Each tool on
its own fastest deterministic path, no LLM anywhere. Cold gather best-of-3,
query median-of-5.

| corpus | ctx-optimize | CodeGraph | graphify | GitNexus |
|---|---|---|---|---|
| **linux v6.9** · 84,300 files | **118.18s** / 4,039ms | 289.86s / **880ms** | 527.72s / 22,799ms\* | **did not finish** (>45 min) |
| graphify-src · 1,476 files | **0.648s** / 27ms | 1.323s / 101ms | 5.123s / 373ms | 10.649s / 779ms |
| ctx-optimize-src · 409 files | **0.326s** / 12ms | 0.762s / — | 1.352s / 120ms | 12.85s / — |
| flask · 344 files | **0.314s** / 12ms | 0.438s / 102ms | 0.845s / 106ms | 6.355s / 794ms |
| gin · 253 files | **0.342s** / 11ms | 0.593s / — | 0.777s / 110ms | 7.56s / — |

*gather / free-text query. `—` = not measured. GitNexus burned 137 CPU-minutes
with a 36 GB heap on the kernel and produced no index; that is recorded as a
non-finish, not as a win for anyone.*

On the kernel, ctx-optimize emits **2,849,719 nodes in a 2.0 GB store**;
CodeGraph 1,838,442 in 4.1 GB; graphify 910,778 in 3.1 GB.

**Where we lose: free-text query LATENCY at scale.** CodeGraph answers a kernel
query in 880ms against our 4,039ms — 4.6x — because 54% of their 4.1 GB is
B-tree index. (An earlier draft said 536ms; that was CodeGraph answering a
single word while every other tool answered the full phrase. Fixed in the
harness.) They are faster and, on the five kernel questions below, less useful:
Ours is 20%, and it currently accelerates exact symbol lookup only — `card` on
the kernel went 1.8s → **under 20ms** — not the lexical ranking `query` runs. A
postings index for `query` is not built and not claimed.

\* **graphify cannot query the kernel graph at default settings.** It builds
the 1.2 GB `graph.json`, then querying it fails with `exceeds
536_870_912-byte cap`. The 22,799ms is only reachable after raising
`GRAPHIFY_MAX_GRAPH_BYTES` by hand.

### Query: speed *and* relevance, on the same questions

Five real kernel questions, linux v6.9, median of 3. Grading rule, applied
identically: **does the top hit name a symbol actually related to the question,
with `file:line`?** The hit is shown so you can judge it yourself.

| question | ctx-optimize | CodeGraph | graphify |
|---|---|---|---|
| mq deadline dispatch request | **3.92s** ✅ `dd_dispatch_prio_aged_requests` block/mq-deadline.c:564 | 0.88s ❌ `struct request` | 23.10s ❌ `u64` |
| ext4 write iter | **3.54s** ✅ `ext4_buffered_write_iter` fs/ext4/file.c:285 | 0.78s ❌ `function iter` | 22.45s ❌ `u32` |
| spinlock irqsave | **3.80s** ✅ `__raw_spin_lock_irqsave` | 0.59s ❌ `struct spinlock` | 23.07s ❌ `u32` |
| tcp congestion control | **3.70s** ✅ `proc_tcp_available_congestion_control` | 0.79s ❌ `struct tcp` | 23.69s ❌ `u64` |
| page allocation failure | 3.60s ❌ `MLX5_…ALLOCATION_FAIL` | 0.81s ❌ *(empty)* | 22.77s ❌ `kcalloc()` |
| **median / useful top hit** | **3.70s · 4 of 5** | 0.79s · **0 of 5** | 23.07s · **0 of 5** |

ripgrep runs these in 1.59s and returns matching **lines** — genuinely useful,
a different artifact, so it isn't scored against a symbol rule.

**CodeGraph is 4.7× faster and got none of them.** FTS5 OR-matches each word and
ranks by frequency, so a multi-word question returns the generic struct literally
named `request` / `tcp` / `spinlock`. graphify returns `u64`/`u32` from
`netfilter/x_tables.h` for four different questions — a constant, not an answer.

We are the slowest graph tool that answers the question. Caveats: this is a
5-question judged sample against a stated rule, not a blind graded run like the
one below — and **we got one of five wrong too.**

### Answer quality — graded, not asserted

gorilla/mux, 12 hand-verified questions, 3 runs, n=36 answers per arm,
`gpt-4o-mini`. Scored deterministically against expected facts; the grader
never sees which arm produced the answer.

| | shell (ripgrep) | **ctx-optimize** | graphify |
|---|--:|--:|--:|
| **correctness** | 35% | **67%** | 40% |
| · impact — "who calls this" (8q) | 29% | **79%** | 42% |
| · locate — "where is X" (4q) | **47%** | 42% | 36% |
| false claims | 0 | **0** | 1 |
| empty answers | 4 | **0** | 0 |
| tool calls / run | 42.7 | **15.0** | 26.0 |
| cost / run | $0.0051 | **$0.0040** | $0.0070 |

**grep wins "where is X". We win "what breaks if I change it."** Two caveats
that ride with these numbers: part of the gap is cheap-model weakness rather
than a tool ceiling — re-run on a frontier model before quoting it as a
ceiling — and a known defect (ambiguous method names collapse the call graph)
means the 79% was scored *despite* a live bug, not because the graph is
complete. Method, every failure, and the defects:
[proof/agent/RESULTS-QUALITY.md](proof/agent/RESULTS-QUALITY.md).

### ripgrep is faster than us, and that's fine

On raw latency, grep-class tools win: on flask, **ripgrep 11ms and ast-grep
17ms vs our 12ms** (plain grep: 253ms); on graphify-src, rg 23ms and ast-grep
56ms vs our 27ms. It's true and it isn't the point: ripgrep returns **matching
lines**; ctx-optimize returns a **resolved symbol** — signature, callers,
callees, blast radius, each with `file:line`. `rg` cannot answer "who calls
this". Exhaustive literal-string sweeps stay grep's job, and we tell agents
exactly that in the shipped instructions card.

Raw data and methodology: [`benchmarks/`](benchmarks/). Agent-level harness
(same model, three ways, provider's own accounting) and the model ladder:
[`proof/agent/`](proof/agent/) — reproducible from a clean runner via
[`.github/workflows/benchmark.yml`](.github/workflows/benchmark.yml).

## More

[CLI reference](docs/cli.md) · [monorepos](docs/monorepos.md) ·
[native sources](docs/sources.md) · [sharing the store](docs/remote-github.md) ·
[custom adapters](docs/adapters.md) · [grammar & route packs](docs/languages-packs.md) ·
[agent integration](docs/agents.md) · [cookbook](docs/cookbook.md) ·
[troubleshooting](docs/troubleshooting.md) ·
[design & lineage](docs/design.md) · [vision](docs/VISION.md) ·
[standing critique](docs/CRITIQUE.md)

## Community & license

Questions, bugs, or you built something with it? Join
**[AgentNexus](https://discord.gg/V9C2kvHC8D)** — a Discord for people building
with AI agents, `#ctx-optimize` channel.

MIT © 2026 Muthukumaran Navaneethakrishnan · made by [muthuishere](https://github.com/muthuishere).
