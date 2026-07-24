# ADR — honest cross-tool benchmark: ctx-optimize vs the field

Status: DRAFT — 2026-07-24. Owner-directed: run the *whole* field for real, in an
isolated arena, with an adversarial auditor, and publish only what we measured.

## Why

Today the site has ONE measured head-to-head (graphify) and compares the rest of
the field (CodeGraph, GitNexus, potpie, Serena, ast-grep) on architecture + their
own published numbers. Owner wants real, measured rows for every tool we can
honestly stand up — wins AND losses — replacing "[FROM DOCS]" claims with
"[MEASURED]" wherever feasible.

## Honesty contract (non-negotiable — the whole point)

Borrowed from the toolnexus benchmark doctrine, adopted verbatim here:

- Every published number is **[MEASURED]** by the committed runner on the stamped
  machine, or it does not go on the site.
- Competitor capability facts from their own docs are **[FROM DOCS]** + link.
  Judgement calls are **[QUALITATIVE]**.
- A tool that cannot be stood up to do the *same* work is **OMITTED with the
  reason stated** — never given an invented row. A partial-but-real table beats a
  complete-but-fake one.
- Each tool is timed on **its own fastest deterministic path**, and that path is
  named in the row. No strawman configs. If a rival beats us, the row says so.
- An **adversarial auditor** (separate agent) reviews the method + raw numbers
  for anything that unfairly flatters ctx-optimize BEFORE anything is published.

## Isolation (owner-directed)

- No global installs. Each tool is `git clone --depth 1` into
  `~/ctx-bench-arena/<tool>/` and installed **per-repo** (local `npm ci`/`pip`/
  `uv`), so the host stays clean and versions are pinned to the cloned commit.
- ctx-optimize is placed in the same arena (the built `bin/ctx-optimize`), timed
  identically — no home-field advantage in how it's invoked.
- Secrets never enter the arena env (the runner strips `*KEY*/*TOKEN*/*SECRET*/
  *PASSWORD*`), matching the existing `benchmarks/bench.py` guard.

## The field and feasibility (verified 2026-07-24)

| Tool | Clone | Cold-gather CLI | Query CLI | Verdict |
|---|---|---|---|---|
| ctx-optimize | (this repo) | `add <path>` | `query` | baseline |
| graphify | pypi/repo | `update --no-cluster` | `query` | ✅ re-measure |
| CodeGraph (@colbymchenry) | colbymchenry/codegraph | `codegraph init <path>` → `.codegraph/` | `codegraph query` | ✅ full |
| GitNexus | abhigyanpatwari/GitNexus | `gitnexus analyze <path>` → `.gitnexus/` | `gitnexus query` | ✅ full |
| ast-grep | ast-grep/ast-grep | — (no index) | `ast-grep run -p …` | ✅ **query-only**, no gather row |
| Serena | oraios/serena | — (LSP, no persistent graph) | LSP symbol ops | ⚠️ different shape — timed separately, NOT in gather table |
| potpie | potpie-ai/potpie | needs Neo4j service | — | ❌ omit — no fair single-machine row |

## What we measure (three axes, shared corpora)

1. **Cold gather** — nothing indexed → graph ready. Each on its fastest
   deterministic path (no LLM/labeling), path named. Wall clock, best of N,
   warm FS cache.
2. **Footprint** — on-disk store size + runtime deps required (services/DB/model
   key).
3. **Query latency** — median of N of a fixed structural question at fixed budget,
   cold from shell. ast-grep joins HERE (scan-every-time vs our pre-built index) —
   this is the honest "structural search with no store" baseline, and the point of
   the axis is index-once-query-fast, not "ast-grep is bad."

Corpora (pinned, identical for all): `corpus-flask`, `corpus-gin`,
`corpus-ctx-src` (this repo's source), `corpus-graphify-src` (graphify clone,
~12k files). ast-grep/Serena timed only where the axis applies.

### 4. Answer quality — the axis that actually matters (small-model judge)

Speed is worthless if the answer is wrong. So we measure whether each tool lets a
**small model** answer real code questions correctly — this is the "how much of the
answer, and how good" axis the owner asked for.

- **Question set**: ~6–10 questions per corpus (locate / mechanism / impact),
  each referencing a REAL symbol in that corpus, with ground truth derived by
  reading source (`file:line` + the fact). Committed as `questions.json`.
- **Capture (deterministic, by the runner)**: for each question, run each tool's
  CLI query/context/explore and save its RAW output verbatim to a dump file.
  No model in this step — just what the tool would hand an agent.
- **Judge (small model)**: a **Haiku** agent reads (a) the question, (b) the
  tool's captured context, and produces an answer using ONLY that context; then
  scores it against the ground-truth key on two things — **correctness** (right
  file/symbol/mechanism: 0/0.5/1) and **coverage** ("how much of the answer" the
  context actually supported). A **no-tool baseline** (plain file listing / grep
  snippet) is one arm, so the table shows LIFT, not just absolute scores.
- **Also report** the context size each tool fed the model (bytes / est. tokens):
  a tool that scores the same on 5× the tokens is worse, not equal.
- Honesty: if a tool returns nothing usable for a question, that is a 0 with the
  empty output shown — never a charitable fill-in. The judge is small ON PURPOSE
  (cheap, reproducible); it is not the arbiter of taste, only of "did the context
  contain the answer."

## Deliverables

- `~/ctx-bench-arena/` — clones + local installs (runtime, not committed).
- `benchmarks/bench_multi.py` — the committed multi-tool runner (extends
  `bench.py`), emits `results-multi.json` with per-tool version + machine stamp.
- `site/proof/compare/results.json` + methodology (already scaffolded) — the
  published, audited rows.
- `compare.html` / `index.html#numbers` — [FROM DOCS] rows swapped for [MEASURED]
  where produced; omissions stated.

## Agent team (owner-directed)

- **bench-runner** — sets up the arena, clones `--depth 1`, installs per-repo,
  runs the three timing axes per tool, AND captures each tool's raw answer output
  for the question set (deterministic, no model). Isolated so install/run noise
  stays out of the orchestrator context.
- **quality-judge** (small model — Haiku, on purpose) — reads each tool's
  captured context and answers/scores against the ground-truth key (correctness +
  coverage), with a no-tool baseline arm for lift. Cheap and reproducible by
  design.
- **bench-auditor** (adversarial) — audits corpora identity, "fastest path"
  fairness, invocation parity, and every number vs its raw log; flags anything
  that flatters us; sign-off gates publication.
- **docs-builder** — parallel, independent: builds the toolnexus-style content
  pages (concepts / cookbook / use-cases) beyond the hero; touches only new files
  under `site/`, honesty + outcome-first + ships-only rules enforced.

## Success check

- Every site row is reproducible by `bench_multi.py` on the stamped machine.
- At least CodeGraph + GitNexus + graphify carry MEASURED cold-gather + footprint
  rows; ast-grep carries a MEASURED query-latency row; Serena/potpie handled by
  the omission rule with stated reasons.
- The auditor's sign-off note is committed next to the results.
