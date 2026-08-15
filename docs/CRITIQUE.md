# Standing critique — what's good, what's not, and the honest token answer

Written 2026-07-11 at the maintainer's request, from measured spike data only. This
file is the counter-weight to VISION.md — read both.

## Can we reduce token usage? Honest answer: yes, bounded, terrain-dependent.

| Question class | Evidence | Realistic savings |
|---|---|---|
| Locate/mechanism on grep-hostile code | S1d −23% with inferior pointer-lists; cards remove residual reads (S1e) | ~30–45% |
| Conceptual/onboarding | S1c −39% (LLM-deep wiki) | unknown for deterministic wiki — swing variable |
| Modern well-named repos | S1b +31% WORSE | zero/negative — unfixable, never claim |
| Impact analysis | grep answers are WRONG; graphify misses 19% of edges | correctness win, not token win |
| Cross-system (schema/topics/logs) | store answers without live introspection | real, unmeasured, no baseline exists |

**Ceiling:** agent fixed costs (system prompt, reasoning, answer) don't shrink
with better tools — the entire k8s grep bill was 32.5k. **>50% universal is
structurally impossible.** Honest headline: capability first; "30–45% on
legacy/hostile code" as a published-benchmark secondary claim.

## Good (defensible)
1. **Symbol card** — the only feature born from measured waste (S1e 28/28).
2. **Exact edges** — cheap (S3), a correctness capability nobody has.
3. **Zero-dep determinism** (S2) + adapters-without-PRs beats the solo-maintainer
   bottleneck structurally.
4. **Store + hooks portability** — #1751 was real demand.
5. **Spike discipline** — 7 measurements before product code; killed 2 bad products.

## Not good (risks, unresolved)
1. **Deterministic wiki unmeasured; its weakness sits on our strength** —
   legacy code (our terrain) has the worst comments. Partially mitigated by the
   wider harvest (see WIKI tiers below); still must be measured.
2. **Scope exploded in one day** — graph+cards+wiki+LSP+wasm+store+sync+hooks+
   adapters+multi-module+3-platform proof harness = months solo. **The Maya
   portfolio-focus check was never run.** Biggest unmanaged risk.
3. **Hooks are a prompt-injection vector, not just code-execution** — poisoned
   store content becomes agent context. "Inert until approved" covers execution
   only; the content-trust model is undesigned.
4. **Market timing** — harnesses are fixing read-waste themselves; cards could
   be absorbed natively within quarters.
5. **Proof-matrix methodology** — Codex/Devin token reporting isn't comparable
   out of the box; our credibility weapon has a methodology dependency.
6. **"2.5x faster than the next tool" does not name its cohort** (2026-08-14;
   scope corrected 2026-08-15). The claim at README:19 is measured against the
   agent-context graph tools in `benchmarks/suite/tools.json` — graphify,
   codegraph, gitnexus, codegraphcontext — and within that field it holds.
   The defect is that it reads as absolute. **GNU Global (gtags)** builds far
   faster (~37M lines/min claimed) and **Zoekt** indexes the kernel in ~160s;
   both are now listed in the manifest as ADJACENT CATEGORIES with reasons —
   gtags serves a human doing jump-to-definition, Zoekt is a text search box,
   and neither answers "what breaks if I change X". They are not competitors,
   the same way ripgrep is not. **Fix the wording, not the cohort:** say
   "fastest of the agent graph tools we tested" and name them. Leaving it
   absolute invites a "what about gtags?" objection that costs more
   credibility than the sentence buys.
7a. **Every scored question is a code-locate question** (found 2026-08-15).
   All 14 competitor-quality questions and all 20 judged questions per corpus
   ask "where is this symbol/implementation". NOTHING scores boundaries
   (external APIs, env vars, spawned processes), routes, doc→code links, or
   transport shape — i.e. we measure ourselves only on the axis where we are
   third (0.804 vs codegraph 0.86) and not at all on the axes where we are the
   only tool that answers. The engineering cost is worse than the marketing
   one: **an unmeasured capability has no regression net**, so a boundary rule
   could silently stop matching and no score would move. See ADR 13.
7. **The judged scoreboard is self-authored, self-graded and self-floored** —
   20 questions per corpus, written by us, marked by us, against floors we
   set. It is genuinely useful as a regression net and worth ~nothing as
   external evidence; a skeptic discounts it entirely and is right to. An
   independent yardstick (Loc-Bench scores at file/module/function
   granularity, which maps 1:1 onto our node kinds) is the missing piece.
8. **Against SCIP we have no accuracy argument** — scip-* indexers run the real
   type checker, so their edges are compiler-precise where ours are heuristic
   (INFERRED/AMBIGUOUS by construction). Our case there is cost and coverage —
   no build required, works on non-compiling trees, 12 languages out of the
   box — and it must never be dressed up as precision.
9. **Published perf numbers went stale the moment the boundary lane landed** —
   README's 118.18s kernel build predates the +60% full-gather regression
   (ADR 6 / ADR 7). Until that is fixed, the headline number is not
   reproducible from HEAD. Re-measure before it is quoted again.

## Kill criteria (pre-committed)
Thin slice (cards + deterministic wiki) on kernel + one true legacy repo:
**composite <25% on hostile terrain → stop, or pivot to impact-analysis-only**
(S3 alone justifies that smaller tool).

**MEASURED 2026-07-12 (S16, `proof/RESULTS.md`): bar NOT cleared on frontier
harnesses** — Claude Code −0.2%, Codex +3.0% (parity at equal quality); Devin
−42.5% (partial n). The pre-committed consequence is in force: the universal
token-savings claim is dead; the product's measured value is onboarding
traces (−34…49% on frontier CLIs), impact-answer correctness (both frontier
grep arms produced a wrong gatekeeper the store got right), the
weak-harness equalizer effect, and wall time. Pivot positioning accordingly;
fix the D1–D3 defects the proof surfaced before re-measuring.

## The wiki answer (maintainer asked: better wiki, or is graphify-style enough?)
- **Tier 1 (graphify-style index):** enough for navigation only — forfeits the
  conceptual class (the strongest measured axis). Not enough alone.
- **Tier 2 (better deterministic wiki, zero LLM):** widen the harvest beyond doc
  comments to ALL human-authored prose: **commit messages** (legacy code with
  bad comments has years of commit log), **test names** (distilled behavior),
  **log/error strings**, READMEs/ADRs via doc-ref edges, call-shape summaries
  from exact edges. A real page even on comment-free legacy code.
- **Tier 3 (accretive wiki — Karpathy depth without a build-time LLM bill):**
  the wiki GROWS FROM USE. First conceptual answer about community X is paid at
  full price by the host agent — then the skill saves it as the page: binary
  validates every file:line, stamps member_hash, stores. Next asker pays ~2k.
  member_hash change → page flagged stale → next answer refreshes. Distillation
  paid per-question-actually-asked (not 15k communities upfront — the k8s
  lesson), no LLM API ever, LLM proposes / binary disposes.
- Composite spike must measure tier 2 and tier 3 separately.
