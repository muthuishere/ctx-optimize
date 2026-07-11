# Standing critique — what's good, what's not, and the honest token answer

Written 2026-07-11 at the owner's request, from measured spike data only. This
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

## Kill criteria (pre-committed)
Thin slice (cards + deterministic wiki) on kernel + one true legacy repo:
**composite <25% on hostile terrain → stop, or pivot to impact-analysis-only**
(S3 alone justifies that smaller tool).

## The wiki answer (owner asked: better wiki, or is graphify-style enough?)
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
