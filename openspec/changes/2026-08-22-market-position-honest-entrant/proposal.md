# ADR 31 — market position: the honest entrant in a validated, execution-starved lane

Status: DRAFT — written 2026-08-22 after a live market recon (HN Algolia API,
GitHub API, npm downloads; captured via the chrome-agent lane). Sources cited
inline. Counter-weight to docs/CRITIQUE.md #4 ("harnesses are fixing
read-waste themselves") — that risk is real and it sets the clock, not the
verdict.

Date: 2026-08-22
Scope: strategy + positioning. No product code in this change. Docs/site copy
and benchmark work may cite this ADR.

## What the recon found (2026-08-22, measured)

1. **The pain is current and spoken in the market's own words.** Top HN
   comment (2026-08-14): *"Every task, your coding agent starts blind. Before
   it changes anything, it re-explores the repo: grep a term, open a file,
   follow an import, back out, try again."* That is VISION's thesis paragraph,
   written by a customer.
2. **Anti-MCP sentiment is rising and rotating toward us.** HN 2026-08-05:
   *"the main issue with MCP is the entire response ends up in the context
   window"*; 2026-08-04: *"MCP tools flood your context window… converting
   MCP tools into Skills cuts token usage by up to 98%"*. ADR 24 (local CLI,
   never MCP) was contrarian on 2026-07-11; on 2026-08-22 it is tailwind.
   Supporting field data already in ADR 24: code-review-graph called **0/15**
   by Claude Code in the r/ClaudeCode 261-run bench — MCP graph tools are
   being ignored in practice.
3. **The lane is validated but execution-starved.** Twelve-plus Show HN
   launches in this exact category since 2025 (Rig, Vexp, KiroGraph,
   code-review-graph, Llmcc, Loom, CodeGraphContext, Overture, …): almost all
   stalled at 1–3 points. Survivors by execution: CodeGraphContext 4,107⭐,
   potpie 5,696⭐ ($2.2M), Serena 28,340⭐ (LSP-as-MCP — inherits the MCP
   criticism). **The idea is commodity; the measured+fast+deterministic
   version is the open seat.**
4. **We have quiet real traction and near-zero presence.** npm
   @muthuishere/ctx-optimize: **2,851 downloads/month**; GitHub
   muthuishere/ctx-optimize: **0 stars**. People find and use it; nobody
   rallies around it. Distribution, not product, is the bottleneck.

## The position (the lock)

**The honest entrant, in a market of hustlers.** HN's reflexive response to
this category (2026-08-11): *"I am not going to trust a single number thrown
by these AI hustlers."* We are the only contestant that publishes where it
LOSES in the README (ripgrep 47% vs our 42% on locate; CodeGraph 880ms vs our
4.0s), killed its own headline claim when S16 measured −0.2%/+3.0%, and ships
the harness so anyone can re-run the numbers. Nobody else can copy this
without first admitting their numbers.

Positioning sentence: **"79% vs 29% on 'what breaks if I change this' —
grep-armed agent comparison, harness in the repo, run it yourself. Where we
lose is in the README too."**

## The four bets, ranked

### Bet 1 — fix the ambush before the crowd arrives (BLOCKER, days)

CRITIQUE #9: README's 118.18s kernel build predates the +60% full-gather
regression (ADR 6/7). First HN thread that tries to reproduce it fails → the
honest-entrant position dies on arrival. **Fix the regression or re-measure
and re-quote. No public push until this is true.** Same pass: CRITIQUE #6 —
"2.5x faster" must name its cohort ("fastest of the agent graph tools we
tested: graphify, codegraph, gitnexus, codegraphcontext").

### Bet 2 — measure the axes where we are the only answer (HIGH, 1–2 weeks)

CRITIQUE #7a: every scored question is a code-locate question — the axis
where we are THIRD (0.804 vs codegraph 0.86) — and nothing scores boundaries,
routes, doc→code, transport shape. The boundary lane and the 9 infra sources
are the differentiator no competitor has, and they are currently unmeasured
→ unprotected (a rule can silently stop matching; no score moves). Build the
boundary/source question class into the bench (ADR 13's follow-through).
This converts "README bullet #6" into a headline claim nobody else can enter.

### Bet 3 — lead with boundaries + infra, not locate (HIGH, copy only)

The dozen dead Show HNs all pitched "find code cheaper." The unclaimed story:
**"your agent understands the whole SYSTEM — code + Postgres schema + Kafka
topics + S3 buckets + OpenAPI — in one local store, no MCP, no credentials
at rest."** This also rides Bet 2's measurement and the anti-MCP rotation.

### Bet 4 — distribution: the GitHub page is empty at 2.8k dl/mo (MEDIUM, ongoing)

0 stars vs 2,851 monthly downloads means discovery is happening (npm/skill
registry/word of mouth) and conversion to community is not. Minimum: README
→ Show HN launch ONLY after Bet 1; the launch post uses the honest-numbers
structure (headline win, named losses, harness link). The launch is
credible exactly once; spend it after the ambush is fixed.

## What we explicitly do NOT do

- **Chase the locate axis.** codegraph wins it (0.86) and ripgrep wins raw
  speed. We state their wins and stop competing there (already README
  policy; keep it).
- **Add MCP.** ADR 24 is doctrine; the market rotated toward us, not away.
- **Claim token savings.** Dead per S16; stays dead.
- **Quote the Loc-Bench 12-instance slice.** CRITIQUE #7: 66.67% selects
  small repos; the full benchmark is dominated by large repos where we are
  below BM25. Publish the full run with the size split or nothing.

## Kill criteria (pre-committed, in the spirit of CRITIQUE)

- If Bet 1 lands (numbers reproducible) and a honest-framed launch still
  produces no adoption movement in 60 days (downloads flat, stars < 50),
  the standalone-tool thesis is wrong → pivot to impact-analysis-only tool
  (CRITIQUE's fallback) or fold into the desk/skill ecosystem as a component.
- If a frontier harness ships native boundary/impact answering that matches
  our 79% within 10 points on OUR harness, the window has closed → stop
  positioning as a product, keep as personal infra.

## Open questions

1. Does the launch include the dashboard (`serve`) as the visual, or a
   terminal GIF? Boundary census is the most screenshot-able artifact we own.
2. Do we name competitors in the launch post itself (README already does)?
   HN norms punish strawmen; our numbers name them already.
3. Loc-Bench full-run cost — worth budgeting before Bet 2 lands, or after?

---

# Addendum — 2026-09-09: the benchmark page buries the claim and fights the wrong opponent

Status: DRAFT, appended to ADR 31 as Bet 1 + Bet 3 in concrete form. No
product code and no site copy until the owner signs off.

## Two problems, one page

`https://muthuishere.github.io/ctx-optimize/benchmarks/` publishes four facts
out of eight committed result files.

**Problem 1 — wrong opponent.** Three of the four published tables are shaped
against **grep**. Grep is not a competitor; `benchmarks/suite/tools.json`
already says so, filing ripgrep/gtags/Zoekt/SCIP as ADJACENT CATEGORIES. A
line matcher is the status quo an agent falls back to, not the thing we are
trying to beat. Beating it is not a claim — it is the entry ticket. Worse,
the grep framing forces a per-call comparison we lose by construction (a
string search is always faster than a graph lookup), which is how the page
ends up leading with our losses.

**Problem 2 — the claim is never stated.** The three things that are actually
true and actually differentiating are each measured, committed, and absent
from the page:

> **Faster to the answer, cheaper to the answer, and a small model gets
> there — against the other graph tools, on real repos.**

## The claim, in the three measured parts

### Part 1 — a small model reaches near-frontier on the store

`benchmarks/agent-model-bench/` — linux kernel, ~274k-node store, 8 block-layer
questions, blind-judged against withheld golden keys:

| model | score /80 | s/question | tool calls (8 q) |
|---|---|---|---|
| fable 5 | 80 | 24.6 | 23 |
| sonnet 5 | 80 | 17.5 | 25 |
| opus 4.8 | 79 | 19.0 | 22 |
| **haiku 4.5** | **72** | **13.6** | **18** |

**The cheapest tier lands 90% of frontier quality at half the wall time and
the fewest tool calls of any arm.** That is the headline the page does not
have: the graph does the reasoning the model would otherwise pay for, so you
can drop a tier. With the protocol pinned, gpt-4o-mini reaches ~70% of
frontier at ~1/100th the cost ($0.015 for 8 questions) — and 23/80 without
it, which is why the protocol ships in `.ctxoptimize/instructions.md`.

### Part 2 — faster to build than every graph tool, on real repos

`benchmarks/multilang/results-multilang.json`, cold seconds:

| corpus | files | us | CodeGraph | Graphify | GitNexus |
|---|---|---|---|---|---|
| java-spring | 10,142 | **10.1** | 19.2 | 160.2 | 234.5 |
| c-postgres | 5,245 | **4.5** | 8.7 | 26.4 | 44.7 |
| py-django | 3,647 | **1.4** | 1.8 | 9.9 | 13.3 |
| go-kubernetes | 3,125 | **4.5** | 8.4 | 27.1 | 44.4 |
| csharp-efcore | 2,677 | **2.0** | 6.2 | 29.0 | 19.7 |
| ts-typescript | 723 | **1.8** | 3.6 | 21.0 | 17.8 |

Six for six, four languages, no DNF: ≈2x CodeGraph, 7–16x Graphify, 10–23x
GitNexus. Kernel scale agrees — 118.2s vs CodeGraph 289.9s vs Graphify 527.7s,
GitNexus DNF, and we are the only tool that produces a complete kernel graph
at all.

### Part 3 — fewer calls to the answer

15.0 tool calls per session vs Graphify's 26.0, and 79% vs 42% on
"who calls this / what breaks". Restate this against Graphify only; drop the
grep column from the headline.

### Part 4 — what no competitor has at all (owner-directed 2026-09-09)

Parts 1-3 are speed and cost — axes where competitors at least have a row.
These four have no competing row anywhere in `benchmarks/suite/tools.json`,
and none of them appear on the site today.

**a. `boundaries` — the outer surface.** No other tool answers "what does this
system talk to". On this repo: **94 ports** — 35 env vars (1 flagged
SENSITIVE), 24 network hosts, plus spawned binaries and exposed routes, each
cited to `file:line` with EXTRACTED/INFERRED provenance
(`internal/boundaries/`). This is ADR 31 Bet 3's story made concrete: the
agent understands the SYSTEM, not just the code. It is also currently
unmeasured, which is Bet 2 — a rule can silently stop matching and no score
moves.

**b. Custom adapters — the open door.** Nine native connectors (16 schemes:
postgres/mysql/mongo/mssql/redis/kafka/nats/s3/openapi/http) plus a validated
`add --json` door: drop a `.js`/`.py`/`.sh` in `.ctxoptimize/adapters/` and
registration IS the file existing (`docs/adapters.md`). Ticketing systems,
log shapes, proprietary tools, converted docs — anything expressible as nodes
and edges enters without us interpreting it, which is what keeps the core
deterministic while the edges stay open. Competitors ship a fixed extractor
set; there is no equivalent to hand a user.

**c. We do not pollute the repo.** The store lives OUTSIDE the tree
(`~/ctxoptimize/<name>/`). What is committed is `.ctxoptimize/` — on this
repo **52 KB, 9 tracked files**: config, adapters, instructions. Graphify
writes `graphify-out/` INTO the repo — 3.6 MB on a 265-file corpus, sitting
in the working tree. And when the artifact is compared like for like we are
4-30x smaller anyway (`multilang/results-multilang.json`, store MB):

| corpus | us | CodeGraph | Graphify | GitNexus |
|---|---|---|---|---|
| java-spring | **142** | 720 | 726 | 1,747 |
| c-postgres | **44** | 183 | 166 | 1,031 |
| go-kubernetes | **40** | 223 | 165 | 898 |
| csharp-efcore | **26** | 145 | 129 | 522 |
| ts-typescript | **19** | 91 | 32 | 351 |
| py-django | **9** | 39 | 34 | 282 |

"Your teammate pulls a 52 KB config, not a gigabyte of index" is a claim
nobody else can make.

**d. Monorepos are a designed answer, not a limitation.** One store per
module plus a federating navigator (`docs/monorepos.md`): refresh cost tracks
the change (edit one service, ~1-2s, not the whole tree), scope follows cwd
with automatic repo-wide escalation on zero hits, the >50% shrink guard stays
meaningful because scope is stable, and `merge` still materializes one
combined artifact on demand. `up` detects and scans; `scan` honours
`.gitignore` with git's own semantics so build output never becomes a module.
Curated in a committed `config.json`.

**e. Any language on earth, compiled on the fly — no fork, no toolchain.**
12 languages are embedded as tree-sitter-to-WASM (go, python, javascript,
typescript, tsx, java, c, cpp, csharp, rust, zig, sql). Everything else is a
drop-in **pack**, and the binary builds it for you:

```sh
ctx-optimize languages add kotlin                     # 17 known names
ctx-optimize languages add https://github.com/tree-sitter/tree-sitter-haskell
```

Compiled **in pure Go**; zig comes from PATH or is auto-downloaded once,
sha256-verified against ziglang.org's index, into `~/ctxoptimize/toolchain/`
— user-invoked, never a background check, never again. The node-type mapping
is auto-drafted from the grammar's own `node-types.json` and marked
`_review`. Packs live in `~/ctxoptimize/grammars/` (machine) or
`.ctxoptimize/grammars/` (committed, shared with the team — kotlin/swift/dart
ship that way). Two hand-built packs already exist on the maintainer's box
(clojure, cljgo), which is the door working rather than a promise.

And it is **three pack systems, one doctrine — drop-in files, no toolchain,
no fork** (`docs/languages-packs.md`): grammar packs teach new languages,
**route packs** teach your framework's URL-to-handler wiring, **manifest
packs** teach your build tool's dependency files. Plus `decl_rules` for
homoiconic languages, where a project's own defining macros become declared
symbols.

Every competitor ships a fixed language list. The honest framing of the
difference: they support N languages; we support N plus whatever you can
point at a grammar URL — and the same door exists for routes, manifests and
adapters. **Nothing in the extraction path requires us to ship a release for
your stack.**

### The diagram the page is missing

One figure, on the benchmarks or how-it-works page: **the intermediate
layer**. Code nodes on the left; the boundary/port layer in the middle (env
vars, hosts, routes, spawned binaries); the outside world on the right
(Postgres, Kafka, S3, OpenAPI, plus one user-supplied adapter box). Edges
carry EXTRACTED vs INFERRED. The point the figure must land: a code graph
stops at the repo edge, and this one does not — the middle layer is the
product. Inline SVG, theme-aware, no library.


## What must be published alongside it

The position in ADR 31 is "the honest entrant." These stay on the page:

- **Warm/incremental — we lose 6/6 to CodeGraph.** Same multilang file: our
  warm is 1.4–11.7s against CodeGraph's 0.19–0.37s (10–31x), and on five of
  six corpora our warm run is **slower than our own cold run** — we
  re-gather; their `sync` is a real incremental. A cold gather is paid once;
  a warm one is paid on every edit. This is the strongest product signal in
  the whole record and it is not a copy problem.
- **Query at kernel scale**: 4.04s vs CodeGraph ~0.98s on the fair question
  (the 536ms figure was single-word; corrected 2026-08-16 in
  `results-linux-scale.json`).
- **Token savings stay dead** per S16 (−0.2% / +3.0% on frontier). Part 1's
  claim is *model-tier* economy — a cheaper model reaching the same answer —
  which is a different, measured thing. Never merge the two.

## Provenance: none of the sweep is publishable as it stands

- **multilang (Parts 2):** 2026-07-24, our **v0.8.0**, single cold run not
  best-of-3, no loadavg, and it predates `benchmarks/suite/setup.py` +
  `versions.json` → **unpinned**. HEAD is v0.15.0 and v0.14 roughly halved
  gather, so our own numbers are stale-LOW; competitor builds are equally six
  weeks old.
- **agent-model-bench (Part 1):** 2026-07-17, model tiers named as of that
  date. Re-run against current tiers before publishing.
- **results-multi-2026-08-15:** genuinely pinned (`arena_pinned`:
  codegraph/gitnexus/graphify true, codegraphcontext false) but `loadavg`
  8.77 — the repo rule records a loaded box swinging the same A/B +16.8%
  then +4.9%.

**Correction to CLAUDE.md:** its warning that the arena "has no
`versions.json` ⇒ no recorded competitor number is pin-verified" is stale —
`~/ctx-bench-arena/versions.json` exists (2026-08-15) with resolved SHAs. The
small-corpus rows ARE pin-verified; kernel and multilang are not. We are
under-claiming on an obsolete caveat.

## Decision (proposed)

1. **Re-run before re-writing.** multilang under `setup.py` pins at HEAD,
   best-of-3, loadavg recorded, idle box. All 12 corpora are already built in
   `~/ctx-bench-arena/multilang/corpora/`; only 6 were ever measured. Publish
   12/12 or state the gap. Re-run agent-model-bench on current tiers.
2. **Headline = both halves, chained (owner-directed 2026-09-09).** Part 1
   and Part 2 are not competing headlines; they are cause and effect, and the
   page states them as one sentence:

   > **Built fastest. Answered cheapest.**
   >
   > The fastest graph build of any agent-context graph tool on real repos —
   > ~2x CodeGraph, 7-16x Graphify, 10-23x GitNexus, six repos, four
   > languages, no DNF. And once it is built, a cheaper model gets the same
   > answer: on the Linux kernel, Haiku 4.5 scores 72/80 where Opus 4.8
   > scores 79 — at half the wall time and the fewest tool calls of any arm.

   Neither half stands alone. "Fast build" is a tool benchmark and buys
   nothing by itself; "cheap model" is unbelievable without saying what makes
   it possible. The chain IS the product: the graph does the structural
   reasoning up front, once and quickly, so the model no longer pays to
   re-derive it per question — which is why you can drop a tier. Every claim
   we make should be traceable to that sentence.

3. **Restructure the page around the claim, not the opponent.** Section order
   follows the headline: build speed (Part 2) establishes the mechanism,
   model ladder (Part 1) delivers the payoff, fewer calls (Part 3) supports
   it. Warm loss and kernel query loss in the same tables, not footnotes.
   Then Part 4 as the closing section — `boundaries`, adapters, the 52 KB
   repo footprint, monorepos, and on-the-fly grammars/routes/manifests —
   under a heading that says plainly these have no competing row. Part 4's
   unifying line, and the one to lead that section with: **every extension
   point is a drop-in file, so we never have to ship a release for your
   stack.** Adapters, grammars, routes, manifests and monorepo config are
   five instances of one doctrine, not five features. Part 4 needs NO re-run to publish: it is capability and
   footprint, not a timing, so it can ship while Parts 1-2 are re-measured.
   Commission the intermediate-layer diagram with it.
4. **Retire grep from the comparison set** on benchmarks and compare, per
   `tools.json`'s own classification. At most one line stating it is the
   fallback being replaced, not a contestant.
5. **Open incremental gather as its own ADR.** "Why is our warm run slower
   than our cold one" gates whether Part 2 is a durable claim or a
   launch-week one.
6. No site copy moves before (1) — Bet 1's rule, applied to the sweep exactly
   as it applied to the 118s kernel row.

## Open questions for the owner

1. All 12 corpora, or the 6 measured plus a stated gap?
2. Does the warm loss ship with the sweep now, or does incremental land
   first?
3. RESOLVED 2026-09-09 (owner): both, chained — see Decision 2. Remaining
   sub-question: the chained headline needs BOTH halves re-run before it can
   ship (multilang at HEAD/pinned/best-of-3, agent-model-bench on current
   tiers). Does the page hold the whole claim until both land, or publish
   the build half first and add the model ladder after?
